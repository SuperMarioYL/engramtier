package report

import (
	"os"
	"strings"
	"testing"

	"github.com/SuperMarioYL/engramtier/internal/heat"
)

// mkKeys builds n two-token keys with the given counts.
func mkKeys(n int, counts []uint64) (keys []heat.NGramKey, total uint64) {
	keys = make([]heat.NGramKey, n)
	for i := range keys {
		keys[i] = heat.NGramKey{N: 2, Tok: [3]uint32{uint32(i), uint32(i + 1), 0}}
		total += counts[i]
	}
	return keys, total
}

// TestBuildSparseReport constructs a clearly-sparse heat map and checks the
// report text carries the load-bearing line: the cold-region size and the
// sparse verdict holding.
func TestBuildSparseReport(t *testing.T) {
	// 20 keys, hottest 2 cover 18/28 ≈ 64%... bump them so top-2 (10%) ≥ 80%.
	counts := make([]uint64, 20)
	for i := range counts {
		counts[i] = 1
	}
	counts[0] = 40
	counts[1] = 40 // top-2 = 80/58 ≈ 138%... fix total below.
	keys, total := mkKeys(20, counts)
	// Recompute total honestly: 2*40 + 18*1 = 98. top-2 = 80/98 = 81.6% ≥ 80%.
	h := heat.HeatMap{Keys: keys, Counts: counts, TotalLookups: total}

	rep := Build(h, Spec{
		TableBytes:     24 * (1 << 30),
		EntryBytes:     8,
		Coverage:       0.95,
		SparsityThresh: 0.10,
		SparsityMin:    0.80,
	})
	if !rep.Summary.Sparse {
		t.Fatal("Sparse false, want true (top-10% covers ~81% >= 80%)")
	}
	if !strings.Contains(rep.Text, "Cold region") {
		t.Fatal("report text missing Cold region line")
	}
	if !strings.Contains(rep.Text, "HOLDS") {
		t.Fatalf("report should announce the thesis HOLDS, got:\n%s", rep.Text)
	}
	if rep.Summary.ColdBytes <= 0 {
		t.Fatalf("ColdBytes = %f, want > 0", rep.Summary.ColdBytes)
	}
}

// TestBuildDenseReport checks a uniform (non-sparse) map falsifies the thesis.
func TestBuildDenseReport(t *testing.T) {
	counts := make([]uint64, 20)
	for i := range counts {
		counts[i] = 5 // uniform → top-10% covers ~10% << 80%
	}
	keys, total := mkKeys(20, counts)
	h := heat.HeatMap{Keys: keys, Counts: counts, TotalLookups: total}

	rep := Build(h, Spec{
		TableBytes:     24 * (1 << 30),
		EntryBytes:     8,
		Coverage:       0.95,
		SparsityThresh: 0.10,
		SparsityMin:    0.80,
	})
	if rep.Summary.Sparse {
		t.Fatal("Sparse true, want false (uniform table is dense)")
	}
	if !strings.Contains(rep.Text, "FALSIFIED") {
		t.Fatalf("report should announce the thesis FALSIFIED, got:\n%s", rep.Text)
	}
}

// TestWriteRoundTrips writes a report to a temp file and confirms the summary
// block is present (the serve command reads it back as key=value lines).
func TestWriteRoundTrips(t *testing.T) {
	h := heat.HeatMap{
		Keys:         []heat.NGramKey{{N: 2, Tok: [3]uint32{1, 1, 0}}},
		Counts:       []uint64{10},
		TotalLookups: 10,
	}
	rep := Build(h, Spec{TableBytes: 100, EntryBytes: 8, Coverage: 0.95})
	path := t.TempDir() + "/profile.report"
	if err := Write(path, rep); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(data)
	for _, want := range []string{"hot_keys=", "cold_bytes=", "sparse=", "sparsity_note="} {
		if !strings.Contains(s, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}
