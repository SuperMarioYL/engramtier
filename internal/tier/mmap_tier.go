package tier

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

// MMapTier is the SSD-mapped cold region of the n-gram table. The hot working
// set (selected offline from a heat map) stays RAM-resident in the caller;
// everything else is paged from this mmap'd file on demand, through the LRU.
//
// The tier maps a fixed-stride table: entry i lives at offset i*entryBytes in
// the backing file. A cold lookup is a bounded slice of the mmap — the OS
// pages only the touched region in, which is the whole point of SSD offload:
// the cold majority never needs to be resident, only addressable.
type MMapTier struct {
	path       string
	entryBytes int64
	count      int64 // number of entries the file holds

	mu    sync.RWMutex
	data  []byte
	file  *os.File
	closed bool
}

// NewMMapTier opens (or creates) the cold-region file at path sized for count
// entries of entryBytes each, and maps it into memory. The file is created
// zeroed if absent so a fresh serve boot has a valid (if cold) region; a
// real run populates it by spilling cold entries from the hot loader. When
// entryBytes <= 0 a sensible default (8) is used.
func NewMMapTier(path string, count int64, entryBytes int64) (*MMapTier, error) {
	if count <= 0 {
		return nil, fmt.Errorf("tier: entry count must be positive, got %d", count)
	}
	if entryBytes <= 0 {
		entryBytes = 8
	}
	size := count * entryBytes

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tier: open cold region %q: %w", path, err)
	}
	// Grow to the full size if it was just created (or is short). Truncate is
	// a no-op when the file already matches.
	if fi, err := f.Stat(); err == nil && fi.Size() < size {
		if err := f.Truncate(size); err != nil {
			f.Close()
			return nil, fmt.Errorf("tier: size cold region %q: %w", path, err)
		}
	}

	// MAP_PRIVATE would detach our writes; we need a shared mapping so a paged-
	// in cold entry is visible to the next lookup. On macOS and Linux this is
	// syscall.MAP_SHARED.
	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tier: mmap %q: %w", path, err)
	}

	return &MMapTier{
		path:       path,
		entryBytes: entryBytes,
		count:      count,
		data:       data,
		file:       f,
	}, nil
}

// Get returns the entry bytes at index idx by slicing the mmap. It does no
// caching — callers route cold lookups through an LRU so a repeat miss is
// cheap. idx out of range is an error, not a panic.
func (t *MMapTier) Get(idx int64) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.closed {
		return nil, errors.New("tier: cold region closed")
	}
	if idx < 0 || idx >= t.count {
		return nil, fmt.Errorf("tier: index %d out of range [0,%d)", idx, t.count)
	}
	off := idx * t.entryBytes
	// Return a copy so the caller cannot mutate the mmap'd backing store
	// through the returned slice (the next lookup expects a pristine region).
	out := make([]byte, t.entryBytes)
	copy(out, t.data[off:off+t.entryBytes])
	return out, nil
}

// Put writes entry bytes at index idx into the mmap'd file. Used by the spill
// path when a cold entry is first materialized (and by tests to seed a region).
func (t *MMapTier) Put(idx int64, b []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return errors.New("tier: cold region closed")
	}
	if idx < 0 || idx >= t.count {
		return fmt.Errorf("tier: index %d out of range [0,%d)", idx, t.count)
	}
	if int64(len(b)) != t.entryBytes {
		return fmt.Errorf("tier: entry size %d != stride %d", len(b), t.entryBytes)
	}
	off := idx * t.entryBytes
	copy(t.data[off:off+t.entryBytes], b)
	return nil
}

// EntryBytes returns the per-entry stride the tier was opened with.
func (t *MMapTier) EntryBytes() int64 { return t.entryBytes }

// Count returns the number of entries the cold region holds.
func (t *MMapTier) Count() int64 { return t.count }

// Close unmaps the region and closes the backing file. Safe to call once;
// further Get/Put return an error rather than touching freed memory.
func (t *MMapTier) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	if t.data != nil {
		if err := syscall.Munmap(t.data); err != nil {
			// Still close the file; report the munmap failure.
			_ = t.file.Close()
			return fmt.Errorf("tier: munmap %q: %w", t.path, err)
		}
		t.data = nil
	}
	return t.file.Close()
}

// TieredLookup pairs the cold mmap region with an LRU hot cache and routes a
// lookup through both: RAM-resident hot keys short-circuit before this struct
// is ever called; a cold key checks the LRU, then pages from the mmap. It is
// the unit the serve command composes with the hot set in m2.
type TieredLookup struct {
	Cold *MMapTier
	Cache *LRUCache
}

// Lookup returns the entry at idx: LRU hit first, then mmap. A miss populates
// the cache so the next access to the same cold key is RAM-fast. The returned
// bool is true when the LRU absorbed the lookup (a hit).
func (l *TieredLookup) Lookup(idx int64) ([]byte, bool, error) {
	if l.Cache != nil {
		if v, ok := l.Cache.Get(uint64(idx)); ok {
			return v, true, nil
		}
	}
	v, err := l.Cold.Get(idx)
	if err != nil {
		return nil, false, err
	}
	if l.Cache != nil {
		l.Cache.Put(uint64(idx), v)
	}
	return v, false, nil
}

// Compile-time guard that MMapTier is an io.Closer — the serve path defers
// Close on the tier regardless of the concrete type.
var _ io.Closer = (*MMapTier)(nil)
