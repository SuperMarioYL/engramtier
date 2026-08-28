package tier

import (
	"path/filepath"
	"testing"
)

// TestLRUHitMissAndGet verifies the cache returns hits and records misses, and
// that an accessed entry moves to most-recently-used.
func TestLRUHitMissAndGet(t *testing.T) {
	c := NewLRU(2)
	if v, ok := c.Get(1); ok || v != nil {
		t.Fatal("Get on empty should miss")
	}
	c.Put(1, []byte{0xAA})
	c.Put(2, []byte{0xBB})

	if v, ok := c.Get(1); !ok || v[0] != 0xAA {
		t.Fatalf("Get(1) hit failed: %v %v", v, ok)
	}
	// Accessing 1 made 2 the LRU; insert 3 evicts 2.
	c.Put(3, []byte{0xCC})
	if _, ok := c.Get(2); ok {
		t.Fatal("Get(2) should miss (evicted)")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("Get(1) should still hit")
	}
	// Sequence: Get(1)miss, Put1, Put2, Get(1)hit, Put3, Get(2)miss, Get(1)hit.
	hits, miss := c.Stats()
	if hits != 2 || miss != 2 {
		t.Fatalf("Stats = (%d,%d), want (2,2)", hits, miss)
	}
}

// TestLRUPutUpdate verifies re-putting an existing key updates the value and
// refreshes recency without growing the cache.
func TestLRUPutUpdate(t *testing.T) {
	c := NewLRU(2)
	c.Put(1, []byte{1})
	c.Put(2, []byte{2})
	c.Put(1, []byte{9}) // update, 2 still LRU
	if v, _ := c.Get(1); v[0] != 9 {
		t.Fatalf("updated value = %v, want 9", v)
	}
	c.Put(3, []byte{3}) // evicts 2
	if _, ok := c.Get(2); ok {
		t.Fatal("Get(2) should miss after eviction")
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

// TestLRUHitRateNoTraffic returns 0 with no lookups.
func TestLRUHitRateNoTraffic(t *testing.T) {
	c := NewLRU(4)
	if r := c.HitRate(); r != 0 {
		t.Fatalf("HitRate = %f, want 0", r)
	}
}

// TestMMapTierGetPut round-trips entries through the mmap'd cold region and
// checks bounds checking rejects out-of-range indices.
func TestMMapTierGetPut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cold.bin")
	tr, err := NewMMapTier(path, 4, 4) // 4 entries x 4 bytes
	if err != nil {
		t.Fatalf("NewMMapTier: %v", err)
	}
	defer tr.Close()

	if err := tr.Put(0, []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("Put(0): %v", err)
	}
	v, err := tr.Get(0)
	if err != nil {
		t.Fatalf("Get(0): %v", err)
	}
	if v[0] != 1 || v[3] != 4 {
		t.Fatalf("Get(0) = %v, want [1 2 3 4]", v)
	}
	// Returned slice must be a copy (mutating it must not affect the region).
	v[0] = 0xFF
	v2, _ := tr.Get(0)
	if v2[0] != 1 {
		t.Fatal("Get returned a live mmap slice, not a copy")
	}
	if _, err := tr.Get(4); err == nil {
		t.Fatal("Get(4) should be out of range")
	}
	if err := tr.Put(0, []byte{1, 2}); err == nil {
		t.Fatal("Put with wrong size should error")
	}
}

// TestMMapTierCloseGuards verifies that Get/Put after Close fail rather than
// touching unmapped memory.
func TestMMapTierCloseGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cold.bin")
	tr, err := NewMMapTier(path, 2, 8)
	if err != nil {
		t.Fatalf("NewMMapTier: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tr.Put(0, make([]byte, 8)); err == nil {
		t.Fatal("Put after Close should error")
	}
	if _, err := tr.Get(0); err == nil {
		t.Fatal("Get after Close should error")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close should be no-op, got %v", err)
	}
}

// TestMMapTierPersists verifies writes survive a reopen (mmap is backed by the
// file), which is the property the serve path relies on for cold residency.
func TestMMapTierPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cold.bin")
	tr, err := NewMMapTier(path, 2, 4)
	if err != nil {
		t.Fatalf("NewMMapTier: %v", err)
	}
	if err := tr.Put(1, []byte{9, 9, 9, 9}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tr2, err := NewMMapTier(path, 2, 4)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer tr2.Close()
	v, err := tr2.Get(1)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if v[0] != 9 {
		t.Fatalf("persisted value = %v, want 9", v)
	}
}

// TestTieredLookupCacheHit verifies the LRU absorbs a repeat cold lookup so the
// mmap is only read once per key.
func TestTieredLookupCacheHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cold.bin")
	tr, err := NewMMapTier(path, 4, 4)
	if err != nil {
		t.Fatalf("NewMMapTier: %v", err)
	}
	defer tr.Close()
	if err := tr.Put(2, []byte{7, 7, 7, 7}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	tl := &TieredLookup{Cold: tr, Cache: NewLRU(4)}

	v, hit, err := tl.Lookup(2)
	if err != nil || hit || v[0] != 7 {
		t.Fatalf("first Lookup = %v %v %v, want miss/7", v, hit, err)
	}
	v2, hit2, err := tl.Lookup(2)
	if err != nil || !hit2 || v2[0] != 7 {
		t.Fatalf("second Lookup = %v %v %v, want hit/7", v2, hit2, err)
	}
	// One miss, one hit so far.
	hits, miss := tl.Cache.Stats()
	if hits != 1 || miss != 1 {
		t.Fatalf("Stats = (%d,%d), want (1,1)", hits, miss)
	}
	// Out-of-range lookup errors through the tier.
	if _, _, err := tl.Lookup(99); err == nil {
		t.Fatal("Lookup(99) should error")
	}
}
