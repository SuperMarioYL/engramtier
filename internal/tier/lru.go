// Package tier implements the cold-tier runtime: the n-gram table is split
// into a RAM-resident hot working set (selected offline from a heat map) and
// an SSD-mapped cold region served through a small LRU. The hot set is a
// static split computed offline (m1 produces it); the LRU only absorbs
// repeated cold lookups so a cold key touched twice does not pay NVMe latency
// twice.
//
// Both pieces live here so the cold-tier's invariants (LRU capacity, mmap
// lifetime, hit/miss accounting) have one owner. The CLI serve command wires
// them into the lookup path in m2; m1 ships them as testable units.
package tier

import (
	"container/list"
	"sync"
)

// LRUCache is a fixed-capacity least-recently-used cache of n-gram lookup
// results. It sits in front of the mmap'd SSD cold region: a hit returns
// without touching NVMe; a miss falls through to the tier (which pages the
// entry in and then inserts it here). It is goroutine-safe — the serve path
// serves lookups back to llama.cpp from potentially concurrent decode steps.
type LRUCache struct {
	capacity int

	mu    sync.Mutex
	index map[uint64]*list.Element
	order *list.List
	hits  uint64
	miss  uint64
}

// entry is a list element value: the key it caches and its result.
type entry struct {
	key    uint64
	result []byte
}

// NewLRU returns a cache holding at most capacity entries. When capacity is
// exceeded the least-recently-used entry is evicted.
func NewLRU(capacity int) *LRUCache {
	if capacity < 1 {
		capacity = 1
	}
	return &LRUCache{
		capacity: capacity,
		index:    make(map[uint64]*list.Element, capacity),
		order:    list.New(),
	}
}

// Get returns the cached result for key and true on a hit. On a hit the entry
// is moved to the front (most-recently-used) so the eviction order tracks
// real access recency. A miss reports (nil, false) and increments the miss
// counter — the caller is expected to page the value in via Put.
func (c *LRUCache) Get(key uint64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		c.order.MoveToFront(el)
		c.hits++
		return el.Value.(*entry).result, true
	}
	c.miss++
	return nil, false
}

// Put inserts a result for key, evicting the least-recently-used entry when at
// capacity. A Put for an existing key updates its value and refreshes its
// recency. The result slice is stored by reference — callers must not mutate
// it after insertion.
func (c *LRUCache) Put(key uint64, result []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		el.Value.(*entry).result = result
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&entry{key: key, result: result})
	c.index[key] = el
	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.index, oldest.Value.(*entry).key)
		}
	}
}

// Stats returns cumulative hits and misses since the cache was created. The
// serve command prints the hit rate to quantify how much NVMe latency the LRU
// absorbed.
func (c *LRUCache) Stats() (hits, miss uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.miss
}

// Len reports the current number of cached entries.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// HitRate returns the fraction of Get calls that hit, or 0 with no traffic.
func (c *LRUCache) HitRate() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.hits + c.miss
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}
