package probe

import (
	"strings"
	"testing"
)

// TestReadPatch verifies the vendored probe is embedded and carries the
// expected header + the env gate the patched binary keys on.
func TestReadPatch(t *testing.T) {
	b, err := ReadPatch()
	if err != nil {
		t.Fatalf("ReadPatch: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "ENGRAM_TIER_PROFILE") {
		t.Fatal("patch does not reference the ENGRAM_TIER_PROFILE gate")
	}
	if !strings.Contains(s, "engram-tier heat-log v1") {
		t.Fatal("patch does not emit the heat-log header the collector validates")
	}
	if !strings.HasPrefix(s, "# EngramTier llama.cpp") {
		t.Fatal("patch missing its descriptive header")
	}
}

// TestPatchNameExists confirms the embedded asset name matches the constant
// the CLI references, so a rename cannot silently break patch-llamacpp.
func TestPatchNameExists(t *testing.T) {
	if _, err := patchFS.ReadFile(PatchName); err != nil {
		t.Fatalf("embedded %s not found: %v", PatchName, err)
	}
}
