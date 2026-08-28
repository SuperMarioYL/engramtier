// Package report renders a HeatMap into the profile report a self-hoster reads.
// The m1 deliverable is one line — "top X% of keys → Y% of lookups, offload
// Z GiB to SSD" — derived from the heat map's Pareto curve. The report owns
// the presentation of that result (text + CSV) but not the analysis: it calls
// into internal/heat for every number, so the heat primitive stays the single
// source of truth for "what is hot."
package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/engramtier/internal/heat"
)

// Default thresholds for the printed Pareto curve: the 1/5/10/20% key
// fractions a LocalLLaMA reader scans for when judging "is this sparse."
var DefaultThresholds = []float64{0.01, 0.05, 0.10, 0.20, 0.50}

// Spec configures a Profile report. All fields are required inputs from the
// profile command: tableBytes is the full n-gram table size (the ~24GiB
// Qwen3.8 table), entryBytes the per-entry stride, coverage the lookup fraction
// the hot working set must cover (default 0.95), and sparseThreshold /
// sparseMin the falsification gate (top sparseThreshold of keys must cover at
// least sparseMin of lookups for the cold-tier thesis to hold).
type Spec struct {
	TableBytes      float64
	EntryBytes     float64
	Coverage        float64
	Thresholds      []float64
	SparsityThresh  float64 // key fraction whose coverage is tested
	SparsityMin     float64 // min lookup fraction required to call sparse
	SparsityThreshDesc string // human label, e.g. "top-10%"
}

// ProfileReport is the materialized report: a text block plus the structured
// working-set summary the serve command reads back (--hotset path). Keeping
// the summary machine-readable lets m2's serve consume m1's output without a
// parser for free-form text.
type ProfileReport struct {
	Text     string
	Summary  Summary
}

// Summary is the key result, in machine-readable form. serve loads a report's
// Summary to know the hot set and how many bytes stay RAM-resident.
type Summary struct {
	TotalLookups  uint64
	DistinctKeys  int
	Coverage      float64
	HotKeys       int
	HotFraction   float64
	HotBytes      float64
	ColdBytes     float64
	Sparse        bool
	SparsityNote  string
}

// Build renders a Spec over a HeatMap into a ProfileReport. It performs no I/O;
// callers write Text and Summary to disk (see Write / WriteCSV). The text
// block is the human-facing m1 artifact; the Summary is the serve contract.
func Build(h heat.HeatMap, spec Spec) ProfileReport {
	if spec.Coverage <= 0 {
		spec.Coverage = 0.95
	}
	if spec.Thresholds == nil {
		spec.Thresholds = DefaultThresholds
	}
	if spec.SparsityThresh <= 0 {
		spec.SparsityThresh = 0.10
		spec.SparsityMin = 0.80
		spec.SparsityThreshDesc = "top-10%"
	}
	if spec.SparsityThreshDesc == "" {
		spec.SparsityThreshDesc = fmt.Sprintf("top-%d%%", int(spec.SparsityThresh*100))
	}

	hotKeys, hotFrac, hotBytes := h.WorkingSet(spec.Coverage, spec.EntryBytes)
	cold := h.ColdRegionBytes(spec.Coverage, spec.TableBytes, spec.EntryBytes)
	sparse := h.Sparse(spec.SparsityThresh, spec.SparsityMin)

	curve := h.Sort().SampleCurve(spec.Thresholds)

	var b strings.Builder
	fmt.Fprintf(&b, "EngramTier access-heat report\n")
	fmt.Fprintf(&b, "============================\n\n")
	fmt.Fprintf(&b, "Total lookups:     %s\n", comm(uint64(h.TotalLookups)))
	fmt.Fprintf(&b, "Distinct keys hit: %s\n", comm(uint64(len(h.Keys))))
	fmt.Fprintf(&b, "Table size:         %s\n", heat.FormatGB(spec.TableBytes))
	fmt.Fprintf(&b, "Hot coverage goal:  %.0f%% of lookups\n\n", spec.Coverage*100)

	fmt.Fprintf(&b, "Pareto curve (hottest keys first):\n")
	fmt.Fprintf(&b, "  %-14s %-16s %s\n", "key fraction", "lookup coverage", "bytes hot")
	for _, p := range curve {
		fmt.Fprintf(&b, "  top %-9.1f%%  %-16s %s\n",
			p.KeyFraction*100,
			fmt.Sprintf("%.1f%%", p.LookupFraction*100),
			heat.FormatBytes(float64(p.HotKeys)*spec.EntryBytes))
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Working set @ %.0f%% coverage: %s keys (%.1f%% of distinct) = %s RAM\n",
		spec.Coverage*100, comm(uint64(hotKeys)), hotFrac*100, heat.FormatBytes(hotBytes))
	fmt.Fprintf(&b, "Cold region (offload to SSD):   %s\n", heat.FormatBytes(cold))

	var verdict string
	if sparse {
		verdict = fmt.Sprintf("SPARSE — %s of keys cover >= %.0f%% of lookups: cold-tier thesis HOLDS (m2 gated open).",
			spec.SparsityThreshDesc, spec.SparsityMin*100)
	} else {
		verdict = fmt.Sprintf("NOT SPARSE — %s of keys do NOT cover %.0f%% of lookups: cold-tier thesis FALSIFIED (m2 gated closed).",
			spec.SparsityThreshDesc, spec.SparsityMin*100)
	}
	fmt.Fprintf(&b, "\nSparseness verdict: %s\n", verdict)

	return ProfileReport{
		Text: b.String(),
		Summary: Summary{
			TotalLookups: h.TotalLookups,
			DistinctKeys: len(h.Keys),
			Coverage:     spec.Coverage,
			HotKeys:      hotKeys,
			HotFraction:  hotFrac,
			HotBytes:     hotBytes,
			ColdBytes:    cold,
			Sparse:       sparse,
			SparsityNote: verdict,
		},
	}
}

// Write persists a report's text block to path. The serve command reads this
// file back when invoked with --hotset <path>. It writes the header and the
// Summary as a small key=value block below the human text so a parser does not
// have to scrape the prose.
func Write(path string, r ProfileReport) error {
	var b strings.Builder
	b.WriteString(r.Text)
	b.WriteString("\n# machine-readable summary (serve reads this back)\n")
	b.WriteString(fmt.Sprintf("total_lookups=%d\n", r.Summary.TotalLookups))
	b.WriteString(fmt.Sprintf("distinct_keys=%d\n", r.Summary.DistinctKeys))
	b.WriteString(fmt.Sprintf("coverage=%s\n", strconv.FormatFloat(r.Summary.Coverage, 'f', -1, 64)))
	b.WriteString(fmt.Sprintf("hot_keys=%d\n", r.Summary.HotKeys))
	b.WriteString(fmt.Sprintf("hot_fraction=%s\n", strconv.FormatFloat(r.Summary.HotFraction, 'f', -1, 64)))
	b.WriteString(fmt.Sprintf("hot_bytes=%s\n", strconv.FormatFloat(r.Summary.HotBytes, 'f', -1, 64)))
	b.WriteString(fmt.Sprintf("cold_bytes=%s\n", strconv.FormatFloat(r.Summary.ColdBytes, 'f', -1, 64)))
	b.WriteString(fmt.Sprintf("sparse=%t\n", r.Summary.Sparse))
	b.WriteString(fmt.Sprintf("sparsity_note=%s\n", r.Summary.SparsityNote))
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// WriteCSV emits the full Pareto curve as CSV (hotkeys, cumlookups,
// key_fraction, lookup_fraction) to w. This is the artifact a benchmark post
// plots; m3's harness also emits per-sweep rows in the same shape.
func WriteCSV(w io.Writer, h heat.HeatMap) error {
	curve := h.Sort().ParetoCurve()
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"hot_keys", "cum_lookups", "key_fraction", "lookup_fraction"}); err != nil {
		return err
	}
	for _, p := range curve {
		if err := cw.Write([]string{
			strconv.Itoa(p.HotKeys),
			strconv.FormatUint(p.CumLookups, 10),
			strconv.FormatFloat(p.KeyFraction, 'f', 6, 64),
			strconv.FormatFloat(p.LookupFraction, 'f', 6, 64),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// comm formats n with thousands separators for readable report lines.
func comm(n uint64) string {
	s := strconv.FormatUint(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}
