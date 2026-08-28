# Examples

The fastest path from clone to a visible result, with no 24 GiB model
required. The `--simulate` flag synthesizes a Zipf-distributed n-gram
workload so the profiling path is demonstrable on any laptop.

## Three-line happy path

```bash
go build -o engram-tier ./cmd/engram-tier
./engram-tier profile --simulate --simulate-alpha 1.5 --table-bytes 24GiB --coverage 0.95
./engram-tier bench    --simulate --simulate-alpha 1.5 --table-bytes 24GiB
```

The `profile` line prints the Pareto curve and the sparseness verdict; the
`bench` line sweeps the hot-set coverage and prints the simulated
"lost only X% throughput" table.

## Real workload (replaces the simulated log)

After patching and rebuilding llama.cpp (`engram-tier patch-llamacpp
--llamacpp ~/src/llama.cpp --rebuild`), run the instrumented binary with the
profile gate set, then point EngramTier at the heat log it produced:

```bash
ENGRAM_TIER_PROFILE=1 ENGRAM_TIER_HEAT_LOG=q.heat.log \
  ~/src/llama.cpp/build-engram-tier/bin/llama-speculative -m qwen3.8-q4.gguf -n 512
./engram-tier profile --heat-log q.heat.log --table-bytes 24GiB --out profile.report
./engram-tier serve   --hotset profile.report
```

See `docs/benchmark-methodology.md` for the full reproducibility contract.
