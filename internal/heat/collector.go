// Package heat owns the n-gram access-heat map: the per-workload record of how
// often each distinct n-gram key is looked up during inference, plus the
// derived Pareto / working-set curve. It is the load-bearing primitive of
// EngramTier — the artifact that falsifies (or confirms) the claim that
// Qwen3.8's n-gram table is sparsely accessed.
//
// The heat log is a stream of raw lookups emitted by the instrumented
// llama.cpp probe (one n-gram key per line, see internal/probe). The Collector
// aggregates that stream into a HeatMap, which is the single owner of the
// "which keys are hot" knowledge. Everything downstream (Pareto curve,
// working-set size, cold-region byte estimate, the profile report) derives
// from a HeatMap.
package heat

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxNGramLen is the widest n-gram key the profiler models. Qwen3.8's keyed
// embedding table is indexed by 2-3-token sequences; a fixed-length array
// keeps a key comparable and alloc-free.
const MaxNGramLen = 3

// NGramKey is a 2-3-token index into Qwen3.8's keyed embedding table. It is
// the unit of access heat: each lookup during inference resolves to exactly
// one NGramKey, and the heat map counts how often each key is touched.
type NGramKey struct {
	N   uint8    // 2 or 3 — the number of tokens that form this key
	Tok [MaxNGramLen]uint32
}

// String renders a key as colon-separated token ids, matching the heat-log
// line format (e.g. "128:256").
func (k NGramKey) String() string {
	parts := make([]string, k.N)
	for i := uint8(0); i < k.N; i++ {
		parts[i] = strconv.FormatUint(uint64(k.Tok[i]), 10)
	}
	return strings.Join(parts, ":")
}

// ParseKey parses a colon-separated token-id list into an NGramKey. n must be
// in [2, MaxNGramLen].
func ParseKey(s string) (NGramKey, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > MaxNGramLen {
		return NGramKey{}, fmt.Errorf("heat: malformed n-gram key %q (want 2-%d token ids)", s, MaxNGramLen)
	}
	var k NGramKey
	k.N = uint8(len(parts))
	for i, p := range parts {
		id, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return NGramKey{}, fmt.Errorf("heat: bad token id %q in key %q: %w", p, s, err)
		}
		k.Tok[i] = uint32(id)
	}
	return k, nil
}

// HeatMap is the core data model: the distinct n-gram keys a workload touched,
// the lookup count per key (aligned with Keys), and the total lookups observed.
// Keys and Counts are kept parallel; the Collector is the only writer.
type HeatMap struct {
	Keys         []NGramKey
	Counts       []uint64
	TotalLookups uint64
}

// HeatLogHeader is the first line of a heat log, marking the format version
// so a future probe change can be detected without a silent mismatch.
const HeatLogHeader = "# engram-tier heat-log v1"

// Collector aggregates a stream of raw n-gram lookups into a HeatMap. It owns
// the in-memory map[NGramKey]uint64; callers drive it by feeding lookups and
// then calling HeatMap to materialize the parallel-slice form the analyzer
// consumes. Keeping aggregation here (not in the C++ probe) makes the heat
// logic testable in pure Go and a single source of truth for "what is hot".
type Collector struct {
	counts map[NGramKey]uint64
	total uint64
}

// NewCollector returns a Collector ready to ingest lookups.
func NewCollector() *Collector {
	return &Collector{counts: make(map[NGramKey]uint64)}
}

// Record increments the lookup count for one n-gram key.
func (c *Collector) Record(k NGramKey) {
	c.counts[k]++
	c.total++
}

// RecordN adds n lookups of the same key in one call.
func (c *Collector) RecordN(k NGramKey, n uint64) {
	c.counts[k] += n
	c.total += n
}

// DistinctKeys reports how many unique keys have been recorded.
func (c *Collector) DistinctKeys() int { return len(c.counts) }

// Total reports the total number of lookups recorded.
func (c *Collector) Total() uint64 { return c.total }

// HeatMap materializes the collected lookups into the parallel-slice form.
// Key order is unspecified until Pareto.Sort reorders it by heat.
func (c *Collector) HeatMap() HeatMap {
	h := HeatMap{
		Keys:         make([]NGramKey, 0, len(c.counts)),
		Counts:       make([]uint64, 0, len(c.counts)),
		TotalLookups: c.total,
	}
	for k, n := range c.counts {
		h.Keys = append(h.Keys, k)
		h.Counts = append(h.Counts, n)
	}
	return h
}

// CollectStream reads raw lookups (one NGramKey.String() per line) from r until
// EOF and aggregates them. Blank lines and comments (lines starting with '#')
// are skipped, so a log may carry a leading HeatLogHeader and trailing notes.
func (c *Collector) CollectStream(r io.Reader) error {
	sc := bufio.NewScanner(r)
	// A long generation can emit many lookups; lift the per-line cap so a
	// multi-megabyte log does not abort mid-scan.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, err := ParseKey(line)
		if err != nil {
			return err
		}
		c.Record(k)
	}
	return sc.Err()
}

// LoadLog opens the heat log at path and aggregates every lookup line into the
// collector. It verifies the HeatLogHeader so a stale or foreign file fails
// loudly instead of silently producing an empty map.
func (c *Collector) LoadLog(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	headerSeen := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if !headerSeen {
				if line != HeatLogHeader {
					return fmt.Errorf("heat: unrecognized log header %q (want %q)", line, HeatLogHeader)
				}
				headerSeen = true
			}
			continue
		}
		k, err := ParseKey(line)
		if err != nil {
			return err
		}
		c.Record(k)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !headerSeen {
		return errors.New("heat: log missing header")
	}
	return nil
}
