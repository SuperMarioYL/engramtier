package heat

import (
	"fmt"
	"sort"
)

// Sorted is a HeatMap whose Keys/Counts have been reordered so Counts is
// descending. It is the input to every Pareto / working-set query: sorting
// once lets all coverage thresholds be answered by a single prefix scan.
// Construct it with Sort; never mutate its slices afterward.
type Sorted struct {
	Keys         []NGramKey
	Counts       []uint64
	TotalLookups uint64
}

// Sort returns a Sorted copy of h with Counts in descending order. Ties keep
// the original key order (stable sort) so reports are deterministic across
// runs with identical heat — reproducibility matters for a falsification tool.
func (h HeatMap) Sort() Sorted {
	keys := make([]NGramKey, len(h.Keys))
	counts := make([]uint64, len(h.Counts))
	copy(keys, h.Keys)
	copy(counts, h.Counts)
	// Index sort to avoid swapping the (larger) key slices repeatedly.
	idx := make([]int, len(counts))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return counts[idx[i]] > counts[idx[j]]
	})
	sk := make([]NGramKey, len(idx))
	sc := make([]uint64, len(idx))
	for i, j := range idx {
		sk[i] = keys[j]
		sc[i] = counts[j]
	}
	return Sorted{Keys: sk, Counts: sc, TotalLookups: h.TotalLookups}
}

// Coverage is one point on the Pareto curve: the smallest set of hottest keys
// that accounts for at least CumLookups of all lookups. KeyFraction is that
// set's size divided by the number of distinct keys; LookupFraction is
// CumLookups / TotalLookups. The curve is the sequence of Coverage points for
// every prefix — the report prints a sampled version of it.
type Coverage struct {
	HotKeys       int
	CumLookups    uint64
	KeyFraction   float64
	LookupFraction float64
}

// ParetoCurve returns the full Pareto curve: for every prefix length k in
// [1, n], the cumulative lookup fraction achieved by the k hottest keys. The
// caller usually samples this (e.g. at 1%, 5%, 10%, 20%) rather than printing
// every point.
func (s Sorted) ParetoCurve() []Coverage {
	n := len(s.Counts)
	if n == 0 || s.TotalLookups == 0 {
		return nil
	}
	curve := make([]Coverage, 0, n)
	var cum uint64
	for i, c := range s.Counts {
		cum += c
		curve = append(curve, Coverage{
			HotKeys:        i + 1,
			CumLookups:     cum,
			KeyFraction:    float64(i+1) / float64(n),
			LookupFraction: float64(cum) / float64(s.TotalLookups),
		})
	}
	return curve
}

// SampleCurve returns the Pareto curve sampled at the given key-fraction
// thresholds (e.g. 0.01, 0.05, 0.10, 0.20). For each threshold it returns the
// smallest coverage point whose KeyFraction is >= the threshold. Thresholds
// above 1.0 are clamped. This is what the profile report prints row-by-row.
func (s Sorted) SampleCurve(thresholds []float64) []Coverage {
	curve := s.ParetoCurve()
	if len(curve) == 0 {
		return nil
	}
	out := make([]Coverage, 0, len(thresholds))
	for _, t := range thresholds {
		if t > 1 {
			t = 1
		}
		// Walk to the first prefix reaching this key fraction.
		for _, pt := range curve {
			if pt.KeyFraction >= t {
				out = append(out, pt)
				break
			}
		}
	}
	return out
}

// WorkingSet returns the smallest set of hottest keys whose cumulative lookups
// reach coverage of all lookups. keyFraction is that set's size divided by the
// number of distinct keys touched; byteSize is keyCount * entryBytes (the RAM
// needed to keep the hot set resident). coverage must be in (0, 1]; values >1
// are clamped to 1.
//
// This is the single line a self-hoster reads: "top X% of keys cover Y% of
// lookups, so only byteSize of RAM is hot — the rest is offloadable to SSD."
func (h HeatMap) WorkingSet(coverage, entryBytes float64) (keyCount int, keyFraction, byteSize float64) {
	if coverage > 1 {
		coverage = 1
	}
	if coverage <= 0 || entryBytes < 0 {
		return 0, 0, 0
	}
	s := h.Sort()
	if len(s.Counts) == 0 || s.TotalLookups == 0 {
		return 0, 0, 0
	}
	target := uint64(coverage * float64(s.TotalLookups))
	var cum uint64
	for i, c := range s.Counts {
		cum += c
		if cum >= target {
			keyCount = i + 1
			break
		}
	}
	// coverage rounded down to a lookup boundary: if target was 0, take 1 key.
	if keyCount == 0 {
		keyCount = len(s.Counts)
	}
	keyFraction = float64(keyCount) / float64(len(s.Counts))
	byteSize = float64(keyCount) * entryBytes
	return keyCount, keyFraction, byteSize
}

// HotSet returns the keys belonging to the working set at the given coverage,
// in hottest-first order. Used by the cold-tier runtime to know which keys to
// keep RAM-resident (and which to spill to the SSD-mapped cold region).
func (h HeatMap) HotSet(coverage float64) []NGramKey {
	if coverage > 1 {
		coverage = 1
	}
	s := h.Sort()
	if len(s.Counts) == 0 || s.TotalLookups == 0 {
		return nil
	}
	target := uint64(coverage * float64(s.TotalLookups))
	var cum uint64
	for i, c := range s.Counts {
		cum += c
		if cum >= target {
			return s.Keys[:i+1]
		}
	}
	return s.Keys
}

// ColdRegionBytes returns the bytes offloadable to SSD: the table size minus
// the hot working set's bytes. tableBytes is the full n-gram table size (the
// ~24GiB Qwen3.8 table); entryBytes is bytes per entry (tableBytes divided by
// the total entry count, or a known stride). A non-negative result means the
// hot set genuinely fits in RAM and the remainder is cold.
func (h HeatMap) ColdRegionBytes(coverage, tableBytes, entryBytes float64) float64 {
	if tableBytes < 0 || entryBytes <= 0 {
		return 0
	}
	_, _, hotBytes := h.WorkingSet(coverage, entryBytes)
	cold := tableBytes - hotBytes
	if cold < 0 {
		return 0
	}
	return cold
}

// Sparse reports whether the top threshold-fraction of keys cover at least
// minCoverage of lookups — the falsification test for the sparseness claim.
// m1 prints this; if false, the cold-tier thesis is falsified and m2 is gated.
func (h HeatMap) Sparse(threshold, minCoverage float64) bool {
	s := h.Sort()
	if len(s.Counts) == 0 || s.TotalLookups == 0 {
		return false
	}
	k := int(threshold * float64(len(s.Counts)))
	if k < 1 {
		k = 1
	}
	var cum uint64
	for i := 0; i < k && i < len(s.Counts); i++ {
		cum += s.Counts[i]
	}
	return float64(cum)/float64(s.TotalLookups) >= minCoverage
}

// FormatGB renders a byte count as a human GiB string for the report.
func FormatGB(b float64) string {
	const giB = 1024 * 1024 * 1024
	return fmt.Sprintf("%.1f GiB", b/giB)
}

// FormatBytes renders a byte count with the largest fitting binary unit so a
// sub-GiB hot set is not misreported as "0.0 GiB". The report uses this for
// working-set and cold-region sizes where the magnitudes span KiB..GiB.
func FormatBytes(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GiB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", b/(1<<10))
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}
