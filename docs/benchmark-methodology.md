# Benchmark Methodology — EngramTier

This document is the reproducibility contract behind every number EngramTier
publishes. If a benchmark post claims "lost only X% throughput," this is how
X was measured and how you can re-measure it on your hardware. EngramTier is,
at its core, a falsification tool: its first job is to determine whether
Qwen3.8-Flash-Next's ~24 GiB n-gram lookup table is *actually* sparsely
accessed. A benchmark that cannot be reproduced is not evidence — it is
marketing — so we spell out the model, the harness, and the known caveats.

## 1. What we are measuring

Two quantities, in this order:

1. **Access-heat sparseness** (m1). For a fixed workload, the fraction of
   distinct n-gram keys that account for a given fraction of all lookups.
   The load-bearing number is the hot-set coverage curve: "the top 10% of
   keys cover Y% of lookups." If Y < 80% at the 10% mark, the table is dense
   and the cold-tier thesis is falsified — no SSD offload recovers
   single-digit throughput loss, and EngramTier stops at m1.
2. **Cold-tier throughput loss** (m2/m3). With the cold majority offloaded to
   SSD/NVMe and the hot working set resident in RAM, the tokens/s delta
   versus an all-RAM baseline: "lost only X% throughput."

## 2. The probe and the heat log

EngramTier instruments the keyed n-gram lookup site in llama.cpp's
speculative-decoding n-gram cache (`examples/speculative/llama-ngram-cache.h`).
The probe, applied by `engram-tier patch-llamacpp`, wraps each lookup with an
emitter gated by the `ENGRAM_TIER_PROFILE` environment variable. When the
gate is set, every looked-up n-gram key (the 2–3-token index into the table)
is appended to a heat log at `$ENGRAM_TIER_HEAT_LOG` (default
`engram-tier.heat.log`), one key per line as colon-separated token ids.

The probe deliberately emits **raw lookups**, not aggregated counts.
Aggregation is owned by the Go heat collector (`internal/heat/collector.go`),
which is the single source of truth for "which keys are hot." This keeps the
profiling logic testable in pure Go and avoids a second implementation of the
heat-map semantics in the C++ probe. When `ENGRAM_TIER_PROFILE` is unset the
probe compiles to a single branch never taken, so the same binary serves both
profile and serve modes with zero overhead in serve.

### Heat log format

```
# engram-tier heat-log v1
128:256
128:256
4:91
...
```

The first line is the format-version header the collector validates (a stale
or foreign file is rejected loudly). Each subsequent line is one looked-up
key. The collector skips comments and blank lines and aggregates the stream
into a `HeatMap` (distinct keys + per-key counts + total lookups).

## 3. The Pareto curve and the working set

From the `HeatMap`, `internal/heat/pareto.go` derives:

- **Pareto curve**: for every prefix k of the count-descending key order, the
  cumulative lookup fraction achieved by the k hottest keys. The report prints
  this sampled at 1/5/10/20/50% key fractions.
- **Working set at coverage C**: the smallest set of hottest keys whose
  cumulative lookups reach C of all lookups. Its byte size is
  `keyCount × entryBytes`. The default coverage is 95%.
- **Cold region**: `tableBytes − workingSetBytes`, the bytes offloadable to
  SSD. For Qwen3.8 the table is ~24 GiB; the per-entry stride defaults to a
  1024-dim float32 embedding (4096 bytes) and is configurable via
  `--entry-bytes`.

The sparseness verdict applies the plan's gate: the top 10% of keys must
cover ≥ 80% of lookups. If so, the cold-tier thesis holds and m2 is gated
open; if not, the thesis is falsified and the tool reports it honestly rather
than proceeding.

## 4. The throughput-loss model (simulated, m3)

`internal/bench/harness.go` sweeps the hot-set coverage and, for each point,
estimates the cold-miss throughput loss with an explicit latency model:

| Parameter | Default | Meaning |
|---|---|---|
| Hot latency | 100 ns | a RAM-resident n-gram lookup |
| Cold latency | 50 000 ns | an SSD/NVMe page-in for a cold lookup |
| Lookups per token | 1 | n-gram lookups per generated token (speculative drafts several) |
| Base decode | 25 ms | per-token decode time with the whole table in RAM |

For coverage C, the cold-miss rate is (1 − C), and the simulated per-token
decode time is:

```
baseDecodeNs + lookupsPerToken × (C × hotLatency + (1 − C) × coldLatency)
```

Throughput (tokens/s) is `1e9 / perTokenNs`; loss % is the delta from the
all-RAM baseline. **These numbers are a simulation, not a measurement** —
the parameters are starting priors, not empirical. The point of the harness
is to let a self-hoster reason about the cold-tier tradeoff curve *before*
running m2's live serve on their hardware. Real numbers replace the simulated
column the moment `engram-tier serve` runs against a patched llama.cpp on the
target box.

Every knob is exposed: `--entry-bytes`, `--table-bytes`, and the model's
latency fields are overridable so a user recalibrates from their own serve
run. Recalibration is the expected workflow, not an exception.

## 5. Running a benchmark

### Sparseness (m1) — no model required

The `--simulate` flag synthesizes a Zipf-distributed heat log so the
profiling path is demonstrable without a 24 GiB model:

```bash
engram-tier profile --simulate --simulate-alpha 1.5 --table-bytes 24GiB --csv pareto.csv
```

A real run replaces the simulated log with one produced by the patched
llama.cpp:

```bash
engram-tier patch-llamacpp --llamacpp ~/src/llama.cpp --rebuild
ENGRAM_TIER_PROFILE=1 ENGRAM_TIER_HEAT_LOG=q.heat.log \
  ~/src/llama.cpp/build-engram-tier/bin/llama-speculative -m qwen3.8-q4.gguf -n 512
engram-tier profile --heat-log q.heat.log --table-bytes 24GiB --out profile.report
```

### Throughput sweep (m3)

```bash
engram-tier bench --heat-log q.heat.log --table-bytes 24GiB --csv sweep.csv
```

The text table is the README's benchmark source; `sweep.csv` is the artifact
to plot.

## 6. Known caveats

- **The sparseness claim was, until m1, a single un-benchmarked Reddit
  assertion.** EngramTier exists to test it. If your workload shows a flat
  Pareto curve (top 20% covers < 50%), the cold-tier thesis is falsified for
  your workload — report it, do not tune the alpha until it looks sparse.
- **The probe instruments the speculative-decoding n-gram cache.** Workloads
  that do not exercise that lookup path will produce an empty heat log; this
  is a real signal (that path is not hot for your workload), not a bug.
- **The throughput model is optimistic about NVMe latency.** Cold-miss
  latency varies an order of magnitude across NVMe grades and is sensitive to
  queue depth. Recalibrate `coldLatencyNs` from a live serve run before
  quoting a loss %.
- **The hot split is static and offline.** v0.1 does not rebalance the hot
  set during inference; the working set is computed once from the profile.
  Workloads whose hot set drifts mid-run will see higher cold-miss rates
  than the offline curve predicts.

## 7. What would falsify EngramTier

Per the plan's kill criteria: if, after 30 days on GitHub with the launch
activities, the repo has < 50 stars **and** the m1 benchmark shows the table
is not sparse (top-10% of keys do not cover ≥ 80% of lookups), the project
is killed — the cold-tier thesis is falsified and no SSD offload recovers
single-digit throughput loss. Also killed if llama.cpp merges a generic
per-tensor offload that subsumes the n-gram table before m2 ships: the window
closed.
