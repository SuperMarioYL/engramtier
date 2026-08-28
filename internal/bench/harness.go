// Package bench sweeps the hot-set coverage of a heat map and emits, for each
// coverage point, the resulting RAM footprint, SSD offload size, and a
// simulated cold-miss throughput-loss estimate. It is the m3 artifact that
// produces the README benchmark table ("sweep hot-set %, emit a CSV + text
// table").
//
// The throughput model is explicitly a simulation — its parameters (RAM vs
// NVMe lookup latency, lookups per token, baseline decode time) are inputs, not
// measured. Real numbers come from m2's live serve; until then the harness
// lets a self-hoster reason about the cold-tier tradeoff curve before the
// table is re-run on their hardware.
package bench

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/engramtier/internal/heat"
)

// LatencyModel is the simulated cost of one n-gram lookup, in nanoseconds,
// split by residency. HotLatency is a RAM-resident lookup; ColdLatency is an
// SSD-mapped cold lookup (NVMe page-in). LookupsPerToken is how many n-gram
// lookups one generated token issues (speculative decode drafts several).
// BaseDecodeNs is the per-token decode time with the whole table in RAM — the
// denominator against which throughput loss is measured.
type LatencyModel struct {
	HotLatencyNs    float64
	ColdLatencyNs   float64
	LookupsPerToken float64
	BaseDecodeNs    float64
}

// DefaultModel uses ballpark figures for a Qwen3.8 4-bit run on a Mac Studio
// with NVMe-backed cold region: ~100ns RAM lookup, ~50µs NVMe page-in, ~1
// n-gram lookup per token, ~25ms/token all-RAM decode. These are starting
// priors for the simulation, not measurements — the bench command exposes every
// knob so a user recalibrates from their own serve run.
func DefaultModel() LatencyModel {
	return LatencyModel{
		HotLatencyNs:    100,
		ColdLatencyNs:   50_000,
		LookupsPerToken: 1,
		BaseDecodeNs:    25_000_000, // 25 ms
	}
}

// Row is one row of the sweep: at coverage C, how much RAM is hot, how much
// spills to SSD, and the simulated all-RAM vs tiered tokens/s plus the loss %.
type Row struct {
	Coverage        float64
	HotBytes        float64
	ColdBytes       float64
	OffloadFraction float64
	BaselineTPS     float64
	TieredTPS       float64
	LossPercent     float64
}

// Spec configures a sweep: the heat map, table/entry geometry, the coverage
// points to evaluate, and the latency model.
type Spec struct {
	Heat       heat.HeatMap
	TableBytes float64
	EntryBytes float64
	Coverages  []float64
	Model      LatencyModel
}

// Sweep evaluates the coverage points and returns one Row per point, hottest
// coverage first. It reuses the heat map's precomputed sort by materializing
// it once; each row's working set is a prefix scan.
func Sweep(spec Spec) []Row {
	if len(spec.Coverages) == 0 {
		spec.Coverages = []float64{0.50, 0.80, 0.90, 0.95, 0.99}
	}
	s := spec.Heat.Sort()
	rows := make([]Row, 0, len(spec.Coverages))

	baseline := spec.Model
	baselinePerToken := baseline.BaseDecodeNs + baseline.LookupsPerToken*baseline.HotLatencyNs
	baselineTPS := 1e9 / baselinePerToken

	for _, cov := range spec.Coverages {
		if cov > 1 {
			cov = 1
		}
		hotKeys, _, hotBytes := spec.Heat.WorkingSet(cov, spec.EntryBytes)
		cold := spec.TableBytes - hotBytes
		if cold < 0 {
			cold = 0
		}
		offload := 0.0
		if spec.TableBytes > 0 {
			offload = cold / spec.TableBytes
		}

		// Simulated tiered decode: hot lookups pay HotLatency, cold misses
		// (1 - coverage) pay ColdLatency. Coverage is the lookup fraction the
		// hot set serves, so the cold-miss rate is (1 - coverage).
		coldMiss := 1 - cov
		tieredPerToken := baseline.BaseDecodeNs +
			spec.Model.LookupsPerToken*(cov*spec.Model.HotLatencyNs+coldMiss*spec.Model.ColdLatencyNs)
		tieredTPS := 1e9 / tieredPerToken
		loss := 0.0
		if baselineTPS > 0 {
			loss = (baselineTPS - tieredTPS) / baselineTPS * 100
		}

		rows = append(rows, Row{
			Coverage:        cov,
			HotBytes:        hotBytes,
			ColdBytes:       cold,
			OffloadFraction: offload,
			BaselineTPS:     baselineTPS,
			TieredTPS:       tieredTPS,
			LossPercent:     loss,
		})
		_ = hotKeys
		_ = s
	}
	return rows
}

// TextTable renders the sweep as a fixed-width table for the README and stdout.
// It mirrors the CSV columns plus a human-readable byte column.
func TextTable(rows []Row) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %-14s %-14s %-12s %-10s %-10s %s\n",
		"coverage", "hot RAM", "cold SSD", "offload", "base t/s", "tier t/s", "loss")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-9s %-14s %-14s %-12s %-10.1f %-10.1f %.2f%%\n",
			fmt.Sprintf("%.0f%%", r.Coverage*100),
			heat.FormatBytes(r.HotBytes),
			heat.FormatBytes(r.ColdBytes),
			fmt.Sprintf("%.1f%%", r.OffloadFraction*100),
			r.BaselineTPS,
			r.TieredTPS,
			r.LossPercent)
	}
	return b.String()
}

// WriteCSV emits the sweep as CSV to w: coverage, hot_bytes, cold_bytes,
// offload_fraction, baseline_tps, tiered_tps, loss_percent.
func WriteCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"coverage", "hot_bytes", "cold_bytes", "offload_fraction", "baseline_tps", "tiered_tps", "loss_percent"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write([]string{
			strconv.FormatFloat(r.Coverage, 'f', 4, 64),
			strconv.FormatFloat(r.HotBytes, 'f', 0, 64),
			strconv.FormatFloat(r.ColdBytes, 'f', 0, 64),
			strconv.FormatFloat(r.OffloadFraction, 'f', 6, 64),
			strconv.FormatFloat(r.BaselineTPS, 'f', 2, 64),
			strconv.FormatFloat(r.TieredTPS, 'f', 2, 64),
			strconv.FormatFloat(r.LossPercent, 'f', 4, 64),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Write persists the text table to path (the README's benchmark table source).
func Write(path string, rows []Row) error {
	return os.WriteFile(path, []byte(TextTable(rows)), 0o644)
}
