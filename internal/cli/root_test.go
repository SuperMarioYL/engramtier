package cli

import (
	"os"
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

// TestDemoFixturePresent pins the committed fixture the README quickstart
// (`bash docs/demo.sh`), the Usage commands, and docs/demo-results.json all
// reference. v0.1.0 shipped without it, so the quickstart exited 1 with
// "open examples/presentation.heat.log: no such file or directory".
func TestDemoFixturePresent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "presentation.heat.log"))
	if err != nil {
		t.Fatalf("examples/presentation.heat.log missing — the README quickstart cannot run: %v", err)
	}
	content := strings.TrimRight(string(data), "\n")
	want := strings.Join([]string{
		heat.HeatLogHeader,
		"1:2", "1:2", "1:2", "1:2", "1:2", "1:2",
		"2:3", "2:3", "2:3",
		"3:4",
	}, "\n")
	if content != want {
		t.Fatalf("fixture content drifted from the recorded demo input:\n got %q\nwant %q", content, want)
	}
}

// TestProfileOverDemoFixture runs the profile command exactly as
// docs/demo.sh does and asserts the substantive results recorded in
// docs/demo-results.json (10 lookups, 3 distinct keys, NOT SPARSE) plus the
// two rendering fixes of this release: the table size prints in KiB (not
// "0.0 GiB"), and every "top X%" label keeps its percent sign attached.
func TestProfileOverDemoFixture(t *testing.T) {
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{
		"profile",
		"--heat-log", filepath.Join(repoRoot(t), "examples", "presentation.heat.log"),
		"--table-bytes", "64KiB",
		"--entry-bytes", "16",
		"--coverage", "0.9",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("profile over demo fixture: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Total lookups:     10",
		"Distinct keys hit: 3",
		"Table size:         64.0 KiB",
		"Working set @ 90% coverage: 2 keys (66.7% of distinct) = 32 B RAM",
		"NOT SPARSE",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("demo profile output missing %q, got:\n%s", want, got)
		}
	}
	for _, bad := range []string{"0.0 GiB", "top 33.3  %"} {
		if strings.Contains(got, bad) {
			t.Fatalf("demo profile output contains stale rendering %q, got:\n%s", bad, got)
		}
	}
	for _, row := range []string{"top 33.3%", "top 66.7%"} {
		if !strings.Contains(got, row) {
			t.Fatalf("demo profile output missing attached label %q, got:\n%s", row, got)
		}
	}
}
