// Package probe owns the llama.cpp instrumentation that emits the raw n-gram
// lookup stream the heat collector aggregates. The probe is a unified-diff
// patch against a llama.cpp source tree: it wraps the keyed n-gram lookup site
// with a per-key counter that, when ENGRAM_TIER_PROFILE=1, streams every
// looked-up key to a heat log the in-process Go collector reads. When the env
// var is unset the probe is zero-cost (a single branch never taken), so the
// same binary serves both profile and serve modes.
//
// patch.go applies and verifies the patch against a llama.cpp checkout. The
// patch itself is llamacpp-ngram.patch, a vendored unified diff.
package probe

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed llamacpp-ngram.patch
var patchFS embed.FS

// PatchName is the vendored patch file. Exported so the CLI can report the
// path it applied and so tests can locate the embedded asset.
const PatchName = "llamacpp-ngram.patch"

// ErrAlreadyApplied means the patch is already present in the source tree
// (git apply reports it would conflict / a probe marker file exists). This is
// not a failure for the user — it means a prior patch-llamacpp run succeeded.
var ErrAlreadyApplied = errors.New("probe: patch already applied to this llama.cpp tree")

// ReadPatch returns the vendored unified diff. It is the single source of the
// probe text; patch-llamacpp and tests both read it here so the patch cannot
// drift between the embedded asset and a duplicated string.
func ReadPatch() ([]byte, error) {
	return patchFS.ReadFile(PatchName)
}

// markerPath is a sentinel file the probe writes into the llama.cpp tree on
// success so a later patch-llamacpp run can detect an already-patched tree
// without re-running git apply (which would fail noisily).
const markerPath = "engram-tier.probe.applied"

// Applied reports whether the llama.cpp tree at llamacppRoot has the probe
// marker, i.e. whether patch-llamacpp has already run there.
func Applied(llamacppRoot string) bool {
	_, err := os.Stat(filepath.Join(llamacppRoot, markerPath))
	return err == nil
}

// Apply writes the vendored patch to a temp file and runs `git apply` against
// the llama.cpp checkout at llamacppRoot. It refuses to re-apply a tree that
// already carries the marker. On success it writes the marker so subsequent
// runs short-circuit. Use force to re-apply (drops the marker first).
func Apply(llamacppRoot string, force bool) error {
	if llamacppRoot == "" {
		return errors.New("probe: llama.cpp root path is required")
	}
	if _, err := os.Stat(llamacppRoot); err != nil {
		return fmt.Errorf("probe: llama.cpp root not found: %w", err)
	}

	marker := filepath.Join(llamacppRoot, markerPath)
	if Applied(llamacppRoot) {
		if !force {
			return ErrAlreadyApplied
		}
		_ = os.Remove(marker)
	}

	patch, err := ReadPatch()
	if err != nil {
		return fmt.Errorf("probe: read vendored patch: %w", err)
	}
	tmp, err := os.CreateTemp("", "engram-tier-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(patch); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// Prefer git apply (honors the tree's index); fall back to patch(1) for a
	// checkout that isn't a git repo. Both run with the llama.cpp root as cwd.
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "apply", "--whitespace=fix", tmp.Name())
		cmd.Dir = llamacppRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("probe: git apply failed: %w\n%s", err, out)
		}
	} else {
		cmd := exec.Command("patch", "-p1", "-i", tmp.Name())
		cmd.Dir = llamacppRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("probe: patch(1) failed: %w\n%s", err, out)
		}
	}

	if err := os.WriteFile(marker, []byte("engram-tier probe applied\n"), 0o644); err != nil {
		return fmt.Errorf("probe: write marker: %w", err)
	}
	return nil
}

// Revert removes the probe from a llama.cpp checkout. It drops the marker and
// reverses the vendored patch with `git apply -R`. Useful for returning a tree
// to stock before an upstream pull.
func Revert(llamacppRoot string) error {
	if !Applied(llamacppRoot) {
		return errors.New("probe: tree not patched (no marker)")
	}
	patch, err := ReadPatch()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "engram-tier-revert-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(patch); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command("git", "apply", "-R", tmp.Name())
	cmd.Dir = llamacppRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("probe: git apply -R failed: %w\n%s", err, out)
	}
	_ = os.Remove(filepath.Join(llamacppRoot, markerPath))
	return nil
}

// Ensure statisfies embed.FS at compile time — keeps the import honest even
// when only ReadPatch is called.
var _ = fs.ModeDir
