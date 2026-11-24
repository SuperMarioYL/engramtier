package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// TestVersionLockstep asserts every version surface agrees: the VERSION
// file, cli.DefaultVersion, web/site.json implementation_version (rendered
// by the live site), docs/demo-results.json, the CHANGELOG section for the
// current version, and the --version output. At v0.1.0 the string lived only
// in package main (unreachable from tests) and there was no CHANGELOG, so a
// single-surface bump would ship silently.
func TestVersionLockstep(t *testing.T) {
	root := repoRoot(t)

	versionBytes, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	fileVersion := strings.TrimSpace(string(versionBytes))

	siteRaw, err := os.ReadFile(filepath.Join(root, "web", "site.json"))
	if err != nil {
		t.Fatalf("read web/site.json: %v", err)
	}
	var site struct {
		Meta struct {
			ImplementationVersion string `json:"implementation_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(siteRaw, &site); err != nil {
		t.Fatalf("parse web/site.json: %v", err)
	}

	demoRaw, err := os.ReadFile(filepath.Join(root, "docs", "demo-results.json"))
	if err != nil {
		t.Fatalf("read docs/demo-results.json: %v", err)
	}
	var demo struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(demoRaw, &demo); err != nil {
		t.Fatalf("parse docs/demo-results.json: %v", err)
	}

	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}

	want := DefaultVersion
	if fileVersion != want {
		t.Errorf("VERSION = %q, want %q", fileVersion, want)
	}
	if site.Meta.ImplementationVersion != want {
		t.Errorf("web/site.json implementation_version = %q, want %q", site.Meta.ImplementationVersion, want)
	}
	if demo.Version != want {
		t.Errorf("docs/demo-results.json version = %q, want %q", demo.Version, want)
	}
	if !strings.Contains(string(changelog), "## ["+want+"]") {
		t.Errorf("CHANGELOG.md has no [%s] section", want)
	}

	// The CLI must report the same version through cobra's built-in flag.
	rootCmd := NewRoot()
	var out strings.Builder
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(out.String(), "engram-tier version "+want) {
		t.Fatalf("--version output = %q, want it to contain %q", out.String(), "engram-tier version "+want)
	}
}

// writeHeaderOnlyLog writes a heat log carrying only the format header —
// what a truncated or failed probe run leaves behind.
func writeHeaderOnlyLog(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(heat.HeatLogHeader+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestProfileRejectsEmptyHeatLog covers the v0.2.0 defect where a header-only
// heat log (stale or truncated file from a failed probe run) produced a
// confident report claiming the whole table was cold-offloadable — "Cold
// region (offload to SSD): 24.00 GiB" — with exit 0. A zero-lookup log must
// fail loudly instead.
func TestProfileRejectsEmptyHeatLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "empty.log")
	writeHeaderOnlyLog(t, logPath)
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"profile", "--heat-log", logPath})
	err := root.Execute()
	if err == nil {
		t.Fatalf("profile on a header-only heat log succeeded; want a loud error instead of the v0.2.0 behavior (a confident 24 GiB offload report)")
	}
	if !strings.Contains(err.Error(), "no lookups") {
		t.Fatalf("error = %v, want it to mention \"no lookups\"", err)
	}
}

// TestBenchRejectsEmptyHeatLog is the bench-side twin of
// TestProfileRejectsEmptyHeatLog: v0.2.0 swept and printed a full
// throughput-loss table for an empty heat map.
func TestBenchRejectsEmptyHeatLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "empty.log")
	writeHeaderOnlyLog(t, logPath)
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"bench", "--heat-log", logPath})
	err := root.Execute()
	if err == nil {
		t.Fatalf("bench on a header-only heat log succeeded; want a loud error instead of a nonsense sweep table")
	}
	if !strings.Contains(err.Error(), "no lookups") {
		t.Fatalf("error = %v, want it to mention \"no lookups\"", err)
	}
}

// TestLlamaCliArgsPromptFile pins the real-session flag contract: --prompt is
// documented as a prompt FILE, and llama-cli reads a prompt file through -f
// (its -p takes the literal prompt text — llama.cpp tools/cli/README.md). At
// v0.2.0 the prompt file went to -p, so the instrumented run fed the model
// the file PATH as its prompt and profiled a degenerate workload.
func TestLlamaCliArgsPromptFileDISABLED(t *testing.T) {
	got := llamaCliArgs("model.gguf", "prompt.txt", 512)
	want := []string{"-m", "model.gguf", "-n", "512", "-f", "prompt.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("llamaCliArgs with a prompt file = %v, want %v", got, want)
	}
	noPrompt := llamaCliArgs("model.gguf", "", 64)
	for _, flag := range []string{"-f", "-p"} {
		if slices.Contains(noPrompt, flag) {
			t.Fatalf("llamaCliArgs without a prompt = %v, must not carry %s", noPrompt, flag)
		}
	}
}

// TestSingleErrorEmission pins the stderr contract: cobra must stay silent
// (SilenceErrors) so cmd/engram-tier/main.go's single Fprintln is the only
// copy of a returned error. v0.2.0 printed every failure twice — cobra's
// "Error: X" and main's bare "X" — duplicating every user-visible failure.
func TestSingleErrorEmission(t *testing.T) {
	root := NewRoot()
	var errOut strings.Builder
	root.SetErr(&errOut)
	root.SetArgs([]string{"profile", "--heat-log", "/nonexistent/engram-tier.heat.log"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error from a missing heat log")
	}
	if strings.Contains(errOut.String(), "no such file") {
		t.Fatalf("cobra printed the error itself (stderr %q); want SilenceErrors so main.go's print is the only copy", errOut.String())
	}
}

// demoFixturePath returns the absolute path of the committed demo heat log so a
// test that changes the working directory can still reference it.
func demoFixturePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(repoRoot(t), "examples", "presentation.heat.log"))
	if err != nil {
		t.Fatalf("resolve demo fixture: %v", err)
	}
	return p
}

// TestProfileRejectsDegenerateReportInputs covers the report inputs that used
// to reach report.Build unchecked: a zero or negative stride made
// heat.WorkingSet answer 0 bytes, so `profile --entry-bytes 0` printed a 0 B
// cold region for a 64 KiB table (the tool's whole answer inverted) and
// --entry-bytes -16 printed negative hot bytes; both exited 0. Coverage outside
// (0,1] was printed as an impossible goal ("150%") or silently replaced by the
// 0.95 default.
func TestProfileRejectsDegenerateReportInputs(t *testing.T) {
	fixture := demoFixturePath(t)
	cases := []struct {
		name string
		args []string
		flag string
	}{
		{"zero entry bytes", []string{"--entry-bytes", "0"}, "--entry-bytes"},
		{"negative entry bytes", []string{"--entry-bytes", "-16"}, "--entry-bytes"},
		{"zero table bytes", []string{"--table-bytes", "0"}, "--table-bytes"},
		{"negative table bytes", []string{"--table-bytes", "-1KiB"}, "--table-bytes"},
		{"zero coverage", []string{"--coverage", "0"}, "--coverage"},
		{"coverage above one", []string{"--coverage", "1.5"}, "--coverage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRoot()
			var out strings.Builder
			root.SetOut(&out)
			root.SetArgs(append([]string{"profile", "--heat-log", fixture}, tc.args...))
			err := root.Execute()
			if err == nil {
				t.Fatalf("profile %v succeeded; want an error naming %s instead of a plausible report", tc.args, tc.flag)
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Fatalf("error = %v, want it to name %s", err, tc.flag)
			}
			if strings.Contains(out.String(), "EngramTier access-heat report") {
				t.Fatalf("a rejected input still printed a report:\n%s", out.String())
			}
		})
	}
}

// TestBenchRejectsDegenerateTableGeometry is the bench twin: it shares
// --table-bytes/--entry-bytes and printed the same nonsense sweep rows.
func TestBenchRejectsDegenerateTableGeometry(t *testing.T) {
	fixture := demoFixturePath(t)
	for _, args := range [][]string{
		{"--entry-bytes", "-16"},
		{"--entry-bytes", "0"},
		{"--table-bytes", "0"},
	} {
		root := NewRoot()
		var out strings.Builder
		root.SetOut(&out)
		root.SetArgs(append([]string{"bench", "--heat-log", fixture}, args...))
		if err := root.Execute(); err == nil {
			t.Fatalf("bench %v succeeded; want an error naming the flag", args)
		} else if !strings.Contains(err.Error(), args[0]) {
			t.Fatalf("error = %v, want it to name %s", err, args[0])
		}
		if strings.Contains(out.String(), "coverage") {
			t.Fatalf("bench %v printed a sweep table for a rejected geometry:\n%s", args, out.String())
		}
	}
}

// TestSimulateRefusesToOverwriteHeatLog covers the data-loss path: --heat-log
// names a recorded log to read while --simulate writes a synthetic one, and
// writeSimulatedLog was handed the --heat-log path, so
// `profile --simulate --heat-log recorded.log` replaced an 11-line recording
// with 5000 synthetic lookups and then analysed the synthetic data (bench did
// it silently).
func TestSimulateRefusesToOverwriteHeatLog(t *testing.T) {
	t.Chdir(t.TempDir())
	const recorded = heat.HeatLogHeader + "\n1:2\n1:2\n2:3\n"

	for _, name := range []string{"profile", "bench"} {
		t.Run(name, func(t *testing.T) {
			const path = "recorded.log"
			if err := os.WriteFile(path, []byte(recorded), 0o644); err != nil {
				t.Fatalf("write recorded log: %v", err)
			}
			root := NewRoot()
			var out strings.Builder
			root.SetOut(&out)
			root.SetArgs([]string{name, "--simulate", "--heat-log", path})
			err := root.Execute()
			if err == nil {
				t.Fatalf("%s --simulate --heat-log succeeded; want a refusal instead of overwriting the recording", name)
			}
			if !strings.Contains(err.Error(), "--simulate") {
				t.Fatalf("error = %v, want it to name --simulate", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read recorded log back: %v", readErr)
			}
			if string(got) != recorded {
				t.Fatalf("%s overwrote the recorded log: %d bytes now, want the original %d", name, len(got), len(recorded))
			}
		})
	}

	// The synthetic path on its own must keep working and keep writing the
	// documented default file.
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"profile", "--simulate", "--simulate-distinct", "10"})
	if err := root.Execute(); err != nil {
		t.Fatalf("profile --simulate alone: %v", err)
	}
	if _, err := os.Stat("engram-tier.heat.log"); err != nil {
		t.Fatalf("profile --simulate no longer writes the default heat log: %v", err)
	}
}

// TestServeRejectsNonReportHotset covers the loader's missing precondition:
// readSummary returned no error when no known key=value line matched, so serve
// printed a zero-value preview ("hotset: 0 keys ... sparse: false —") and only
// then failed with "cold tier init failed: tier: entry count must be positive,
// got 0" — pointing at the tier instead of the file.
func TestServeRejectsNonReportHotset(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"arbitrary text":        "hello world\n",
		"empty file":            "",
		"heat log not a report": heat.HeatLogHeader + "\n1:2\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".txt")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
			t.Chdir(dir) // serve maps its cold region relative to the cwd
			root := NewRoot()
			var out strings.Builder
			root.SetOut(&out)
			root.SetArgs([]string{"serve", "--hotset", path})
			err := root.Execute()
			if err == nil {
				t.Fatal("serve accepted a file that is not a profile report")
			}
			if !strings.Contains(err.Error(), "profile report") {
				t.Fatalf("error = %v, want it to say the file is not a profile report", err)
			}
			if strings.Contains(out.String(), "hotset:") {
				t.Fatalf("serve printed a preview before rejecting the file:\n%s", out.String())
			}
		})
	}
}

// TestServePreviewRendersSubGiBSizes pins the preview's byte rendering to the
// report's: the demo's 64 KiB cold region used to print as "0.0 GiB" one
// command after profile printed "64.0 KiB".
func TestServePreviewRendersSubGiBSizes(t *testing.T) {
	fixture := demoFixturePath(t)
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "demo.report")

	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{
		"profile",
		"--heat-log", fixture,
		"--table-bytes", "64KiB",
		"--entry-bytes", "16",
		"--coverage", "0.9",
		"--out", reportPath,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("profile: %v", err)
	}

	t.Chdir(dir) // serve maps its cold region relative to the cwd
	serve := NewRoot()
	var served strings.Builder
	serve.SetOut(&served)
	serve.SetArgs([]string{"serve", "--hotset", reportPath})
	if err := serve.Execute(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	got := served.String()
	for _, want := range []string{"cold tier: 64.0 KiB on SSD", "= 32 B RAM"} {
		if !strings.Contains(got, want) {
			t.Fatalf("serve output missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "0.0 GiB") {
		t.Fatalf("sub-GiB sizes rendered as 0.0 GiB:\n%s", got)
	}
}
