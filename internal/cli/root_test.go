package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperMarioYL/engramtier/internal/heat"
)

// repoRoot returns the repository root, so tests can reference checked-in
// fixtures (examples/presentation.heat.log, VERSION, web/site.json) without
// depending on the package's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..")
}

// loadSimLog writes a simulated log with the given parameters and loads it
// through the real collector (header validation included).
func loadSimLog(t *testing.T, distinct int, alpha float64) *heat.Collector {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sim.log")
	if err := writeSimulatedLog(path, distinct, alpha); err != nil {
		t.Fatalf("writeSimulatedLog(distinct=%d, alpha=%v): %v", distinct, alpha, err)
	}
	col := heat.NewCollector()
	if err := col.LoadLog(path); err != nil {
		t.Fatalf("LoadLog: %v", err)
	}
	return col
}

// TestWriteSimulatedLogUniformFallback covers the alphas that panicked at
// v0.1.0: math/rand/v2's NewZipf returns nil for s <= 1 and calling Uint64
// on it panicked with "rand: nil Zipf" — on the exact value ("1.0") the
// --simulate-alpha help text recommends. Both must now produce a valid,
// collector-loadable log via the uniform fallback.
func TestWriteSimulatedLogUniformFallback(t *testing.T) {
	for _, alpha := range []float64{1.0, 0.5} {
		col := loadSimLog(t, 50, alpha)
		if col.Total() != 5000 {
			t.Fatalf("alpha %v: total lookups = %d, want 5000", alpha, col.Total())
		}
		if col.DistinctKeys() > 50 {
			t.Fatalf("alpha %v: distinct keys = %d, want <= 50", alpha, col.DistinctKeys())
		}
	}
}

// TestWriteSimulatedLogZipf checks the alpha > 1 path still produces a valid
// log and that the Zipf samples concentrate more than the uniform fallback —
// so the two paths cannot silently swap.
func TestWriteSimulatedLogZipf(t *testing.T) {
	hottest := func(col *heat.Collector) uint64 {
		hm := col.HeatMap()
		var max uint64
		for _, c := range hm.Counts {
			if c > max {
				max = c
			}
		}
		return max
	}
	zipf := loadSimLog(t, 50, 1.5)
	uniform := loadSimLog(t, 50, 1.0)
	if zipf.Total() != 5000 {
		t.Fatalf("zipf total = %d, want 5000", zipf.Total())
	}
	if zipf.DistinctKeys() > 50 {
		t.Fatalf("zipf distinct = %d, want <= 50", zipf.DistinctKeys())
	}
	if hottest(zipf) <= hottest(uniform) {
		t.Fatalf("zipf(1.5) hottest key count %d not above uniform %d — distributions swapped?", hottest(zipf), hottest(uniform))
	}
}

// TestSimulateProfileCommandRuns runs the real profile command end-to-end
// with the previously-panicking alpha, through the cobra command tree (no
// subprocess): v0.1.0 crashed here with a raw panic trace.
func TestSimulateProfileCommandRuns(t *testing.T) {
	t.Chdir(t.TempDir()) // the simulated log defaults to the working directory
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"profile", "--simulate", "--simulate-alpha", "1.0", "--simulate-distinct", "50"})
	if err := root.Execute(); err != nil {
		t.Fatalf("profile --simulate --simulate-alpha 1.0: %v", err)
	}
	if !strings.Contains(out.String(), "EngramTier access-heat report") {
		t.Fatalf("profile output missing report header, got:\n%s", out.String())
	}
}
