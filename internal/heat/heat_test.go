package heat

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestParseKeyRoundTrip checks a key serializes and parses back to itself,
// covering 2- and 3-token keys and the malformed-input rejection.
func TestParseKeyRoundTrip(t *testing.T) {
	cases := []NGramKey{
		{N: 2, Tok: [3]uint32{128, 256, 0}},
		{N: 3, Tok: [3]uint32{1, 2, 3}},
	}
	for _, k := range cases {
		got, err := ParseKey(k.String())
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", k.String(), err)
		}
		if got != k {
			t.Fatalf("ParseKey(%q) = %v, want %v", k.String(), got, k)
		}
	}
	if _, err := ParseKey("1"); err == nil {
		t.Fatal("ParseKey accepted a 1-token key")
	}
	if _, err := ParseKey("a:b"); err == nil {
		t.Fatal("ParseKey accepted non-numeric token")
	}
}

// TestCollectorAggregation feeds a known stream of lookups and checks the
// collector sums counts and totals correctly.
func TestCollectorAggregation(t *testing.T) {
	c := NewCollector()
	a := NGramKey{N: 2, Tok: [3]uint32{1, 2, 0}}
	b := NGramKey{N: 2, Tok: [3]uint32{3, 4, 0}}
	c.Record(a)
	c.Record(a)
	c.RecordN(a, 2) // 4 total for a
	c.Record(b)     // 1 for b

	if c.Total() != 5 {
		t.Fatalf("Total = %d, want 5", c.Total())
	}
	if c.DistinctKeys() != 2 {
		t.Fatalf("DistinctKeys = %d, want 2", c.DistinctKeys())
	}
	hm := c.HeatMap()
	if len(hm.Keys) != 2 || hm.TotalLookups != 5 {
		t.Fatalf("HeatMap = %+v, want 2 keys / 5 lookups", hm)
	}
}

// TestCollectStream verifies the collector aggregates a raw lookup stream and
// skips comments and blanks, including the heat-log header.
func TestCollectStream(t *testing.T) {
	in := "# engram-tier heat-log v1\n1:2\n1:2\n3:4\n\n# trailing note\n"
	c := NewCollector()
	if err := c.CollectStream(bytes.NewBufferString(in)); err != nil {
		t.Fatal(err)
	}
	hm := c.HeatMap()
	if c.Total() != 3 {
		t.Fatalf("Total = %d, want 3", c.Total())
	}
	if len(hm.Keys) != 2 {
		t.Fatalf("distinct keys = %d, want 2", len(hm.Keys))
	}
}

// TestLoadLogHeader verifies a missing or wrong header is rejected so a stale
// log cannot silently produce an empty heat map.
func TestLoadLogHeader(t *testing.T) {
	tmp := t.TempDir() + "/bad.log"
	if err := os.WriteFile(tmp, []byte("1:2\n1:3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCollector()
	if err := c.LoadLog(tmp); err == nil {
		t.Fatal("LoadLog accepted a headerless log")
	}
}

// writeLines is a tiny helper for headerless log fixtures.
func writeLines(path, s string) error { return os.WriteFile(path, []byte(s), 0o644) }

// TestSortDescending checks Sort reorders by descending count, ties stable.
func TestSortDescending(t *testing.T) {
	h := HeatMap{
		Keys: []NGramKey{
			{N: 2, Tok: [3]uint32{1, 1, 0}},
			{N: 2, Tok: [3]uint32{2, 2, 0}},
			{N: 2, Tok: [3]uint32{3, 3, 0}},
		},
		Counts:       []uint64{3, 5, 1},
		TotalLookups: 9,
	}
	s := h.Sort()
	want := []uint64{5, 3, 1}
	for i, c := range s.Counts {
		if c != want[i] {
			t.Fatalf("Count[%d] = %d, want %d", i, c, want[i])
		}
	}
	if s.TotalLookups != 9 {
		t.Fatalf("TotalLookups = %d, want 9", s.TotalLookups)
	}
}

// TestWorkingSet verifies the smallest-hot-set computation at a coverage
// threshold, including the byte size scaling.
func TestWorkingSet(t *testing.T) {
	// 4 keys: counts 5,3,1,1 = total 10. 90% coverage needs 5+3+1 = 9 (3 keys).
	h := HeatMap{
		Keys: []NGramKey{
			{N: 2, Tok: [3]uint32{1, 1, 0}},
			{N: 2, Tok: [3]uint32{2, 2, 0}},
			{N: 2, Tok: [3]uint32{3, 3, 0}},
			{N: 2, Tok: [3]uint32{4, 4, 0}},
		},
		Counts:       []uint64{5, 3, 1, 1},
		TotalLookups: 10,
	}
	keyCount, keyFrac, byteSize := h.WorkingSet(0.9, 8)
	if keyCount != 3 {
		t.Fatalf("keyCount = %d, want 3", keyCount)
	}
	if keyFrac != 0.75 {
		t.Fatalf("keyFraction = %f, want 0.75", keyFrac)
	}
	if byteSize != 24 {
		t.Fatalf("byteSize = %f, want 24", byteSize)
	}
	// 100% coverage needs all keys.
	if kc, _, _ := h.WorkingSet(1.0, 8); kc != 4 {
		t.Fatalf("100%% coverage keyCount = %d, want 4", kc)
	}
}

// TestColdRegionBytes checks the offloadable bytes accounting.
func TestColdRegionBytes(t *testing.T) {
	h := HeatMap{
		Keys:         []NGramKey{{N: 2, Tok: [3]uint32{1, 1, 0}}},
		Counts:       []uint64{10},
		TotalLookups: 10,
	}
	// 1 hot key at 8 bytes; table 100 bytes → 92 cold.
	cold := h.ColdRegionBytes(1.0, 100, 8)
	if cold != 92 {
		t.Fatalf("cold = %f, want 92", cold)
	}
	// If the hot set exceeds the table, cold is clamped to 0.
	cold = h.ColdRegionBytes(1.0, 1, 8)
	if cold != 0 {
		t.Fatalf("cold = %f, want 0 (clamped)", cold)
	}
}

// TestSparse verifies the falsification gate: top-threshold coverage test.
func TestSparse(t *testing.T) {
	// 10 keys; hottest 1 (10%) covers 9/18 = 50%; hottest 2 (20%) cover
	// 10/18 = 55.6%. Total lookups 18.
	h := HeatMap{
		Keys:         makeKeys(10),
		Counts:       []uint64{9, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		TotalLookups: 18,
	}
	if h.Sparse(0.10, 0.80) {
		t.Fatal("Sparse true, want false (top 10% covers 50% < 80%)")
	}
	// Hottest 2 keys (20%) cover 10/18 ≈ 55.6% — still under 80%.
	if h.Sparse(0.20, 0.80) {
		t.Fatal("Sparse true, want false (top 20% covers 55.6% < 80%)")
	}
	// Lower the bar: top 10% covers 50% >= 50% → true.
	if !h.Sparse(0.10, 0.50) {
		t.Fatal("Sparse false, want true (top 10% covers 50% >= 50%)")
	}
}

func makeKeys(n int) []NGramKey {
	ks := make([]NGramKey, n)
	for i := range ks {
		ks[i] = NGramKey{N: 2, Tok: [3]uint32{uint32(i), uint32(i + 1), 0}}
	}
	return ks
}

// TestFormatGB checks the human byte rendering.
func TestFormatGB(t *testing.T) {
	got := FormatGB(3 * (1 << 30))
	if !strings.HasPrefix(got, "3.0 GiB") {
		t.Fatalf("FormatGB(3GiB) = %q, want 3.0 GiB", got)
	}
	if FormatGB(0) != "0.0 GiB" {
		t.Fatalf("FormatGB(0) = %q", FormatGB(0))
	}
}
