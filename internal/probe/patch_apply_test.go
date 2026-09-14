package probe

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These tests guard the vendored llama.cpp probe patch against the three
// failure classes found at v0.1.0: a byte-corrupt diff (wrong hunk counts),
// a target that does not exist upstream, and ill-formed C++ in the inserted
// probe. The old suite only grepped the patch text, so all three shipped.

// parseHunkCounts is a minimal unified-diff structural check. For every
// "@@ -a,b +c,d @@" hunk it counts the body lines (" " and "-" belong to the
// old side, " " and "+" to the new side, "\" is ignored) and asserts they
// match the counts in the header — the invariant `git apply` enforces before
// it will even look at file content ("corrupt patch at line N").
func parseHunkCounts(t *testing.T, diff []byte) {
	t.Helper()
	var file string
	for i, raw := range strings.Split(string(diff), "\n") {
		line := i + 1
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			file = raw
		case strings.HasPrefix(raw, "@@ "):
			oldN, newN, ok := parseHunkHeader(raw)
			if !ok {
				t.Fatalf("line %d: unparseable hunk header %q", line, raw)
			}
			var gotOld, gotNew int
			for _, body := range strings.Split(string(diff), "\n")[i+1:] {
				if body == "" || strings.HasPrefix(body, "@@ ") ||
					strings.HasPrefix(body, "diff --git ") ||
					strings.HasPrefix(body, "--- ") || strings.HasPrefix(body, "+++ ") {
					break
				}
				switch {
				case strings.HasPrefix(body, " ") || body == "":
					gotOld++
					gotNew++
				case strings.HasPrefix(body, "-"):
					gotOld++
				case strings.HasPrefix(body, "+"):
					gotNew++
				case strings.HasPrefix(body, `\`):
					// "\ No newline at end of file" — not a body line.
				default:
					t.Fatalf("%s line %d: diff body line without a ' ', '+' or '-' prefix: %q", file, line, body)
				}
			}
			if gotOld != oldN || gotNew != newN {
				t.Fatalf("%s line %d: hunk header claims -%d/+%d lines but body has %d/%d — git apply would reject this patch as corrupt",
					file, line, oldN, newN, gotOld, gotNew)
			}
		}
	}
}

// parseHunkHeader extracts the old/new line counts from "@@ -a,b +c,d @@ ...".
func parseHunkHeader(s string) (oldN, newN int, ok bool) {
	seg := strings.Split(s, "@@")
	if len(seg) < 3 {
		return 0, 0, false
	}
	ranges := strings.Fields(strings.TrimSpace(seg[1]))
	if len(ranges) != 2 || !strings.HasPrefix(ranges[0], "-") || !strings.HasPrefix(ranges[1], "+") {
		return 0, 0, false
	}
	count := func(r string) (int, bool) {
		parts := strings.SplitN(r[1:], ",", 2)
		if len(parts) == 1 { // "-a" means 1 line
			return 1, true
		}
		n, err := strconv.Atoi(parts[1])
		return n, err == nil
	}
	oldN, ok = count(ranges[0])
	if !ok {
		return 0, 0, false
	}
	newN, ok = count(ranges[1])
	return oldN, newN, ok
}

// TestVendoredPatchStructurallyValid keeps the vendored diff structurally
// sound: every hunk's line counts match its body. v0.1.0 shipped a patch all
// three of whose hunks had wrong counts, so `git apply` failed with "corrupt
// patch at line 63" before the target file was even considered.
func TestVendoredPatchStructurallyValid(t *testing.T) {
	patch, err := ReadPatch()
	if err != nil {
		t.Fatalf("ReadPatch: %v", err)
	}
	if !bytes.Contains(patch, []byte("common/ngram-cache.cpp")) {
		t.Fatal("vendored patch does not target common/ngram-cache.cpp")
	}
	parseHunkCounts(t, patch)
}

// TestVendoredPatchAppliesToFixture checks the vendored patch applies cleanly
// to a byte-exact copy of the upstream file it was generated against
// (llama.cpp master 96ffdc41ceb055e1c2d3d96667ae6d9f0ccb710b). v0.1.0's
// patch targeted examples/speculative/llama-ngram-cache.h, a file that never
// existed in any llama.cpp revision, so no real checkout could ever accept it.
func TestVendoredPatchAppliesToFixture(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	patch, err := ReadPatch()
	if err != nil {
		t.Fatalf("ReadPatch: %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "common"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(git, "init", "-q", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "ngram-cache.cpp"))
	if err != nil {
		t.Fatalf("read fixture: %v (testdata missing?)", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "common", "ngram-cache.cpp"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	patchPath := filepath.Join(dir, "probe.patch")
	if err := os.WriteFile(patchPath, patch, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(git, "apply", "--check", "--whitespace=fix", patchPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vendored patch does not apply to the upstream fixture (upstream drifted?): %v\n%s", err, out)
	}
}

// TestVendoredProbeBlockCompiles extracts the probe block from the vendored
// patch (the added lines between the BEGIN/END markers), wraps it in a
// standalone translation unit with stubbed llama.cpp types, and runs a
// syntax-only compile. v0.1.0's probe used its call counter before its
// declaration and used std::uint64_t without <cstdint> — both hard compile
// errors a syntax check catches. Skipped when no C++ compiler is present.
func TestVendoredProbeBlockCompiles(t *testing.T) {
	var compiler string
	for _, cand := range []string{"g++", "clang++", "c++"} {
		if p, err := exec.LookPath(cand); err == nil {
			compiler = p
			break
		}
	}
	if compiler == "" {
		t.Skip("no C++ compiler available (tried g++, clang++, c++)")
	}

	patch, err := ReadPatch()
	if err != nil {
		t.Fatalf("ReadPatch: %v", err)
	}
	var block []string
	inBlock := false
	for _, raw := range strings.Split(string(patch), "\n") {
		if !strings.HasPrefix(raw, "+") {
			continue
		}
		added := raw[1:]
		switch {
		case strings.Contains(added, "engram-tier access-heat probe (begin)"):
			inBlock = true
		case strings.Contains(added, "engram-tier access-heat probe (end)"):
			inBlock = false
		case inBlock:
			block = append(block, added)
		}
	}
	if len(block) == 0 {
		t.Fatal("vendored patch has no probe block between the BEGIN/END markers")
	}

	tu := `#include <cstdint>
#include <cstdio>
#include <cstdlib>
typedef int32_t llama_token;
#define LLAMA_TOKEN_NULL (-1)
#define LLAMA_NGRAM_MAX 4
` + strings.Join(block, "\n") + `

int main() { engram::probe_emit(nullptr); return 0; }
`
	dir := t.TempDir()
	src := filepath.Join(dir, "probe_smoke.cpp")
	if err := os.WriteFile(src, []byte(tu), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiler, "-std=c++17", "-fsyntax-only", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe block extracted from the vendored patch does not compile: %v\n%s\n--- TU ---\n%s", err, out, tu)
	}
}
