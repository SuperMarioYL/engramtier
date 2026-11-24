# Changelog

All notable changes to EngramTier are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] — 2026-11-25

### Fixed

- **The sparseness verdict tests the same "top X%" key set the report prints.**
  `heat.Sparse` selected `int(threshold*n)` keys (floor) while the printed
  Pareto curve uses the smallest prefix whose key fraction reaches the
  threshold (ceil). On a 19-key log the two disagreed by one key, so `profile`
  printed `top 10.5%  82.5%` and then `NOT SPARSE — top-10% of keys do NOT cover
  80% ... FALSIFIED` in the same report, exit 0. The verdict now uses the
  curve's definition, so the gate and the evidence above it agree.
- **Degenerate report inputs are rejected instead of reporting a 0 B cold
  region.** `--entry-bytes 0` / `-16` and `--table-bytes 0` turned the headline
  "Cold region (offload to SSD)" line into 0 B (with negative "bytes hot" cells)
  for a 64 KiB table and exited 0 — the tool's answer inverted. `--coverage 1.5`
  printed an impossible "150% of lookups" goal and `--coverage 0` was silently
  replaced by the 95% default. `profile` and `bench` now reject the geometry and
  `profile` rejects coverage outside (0, 1], naming the flag.
- **`--simulate` no longer overwrites the log named by `--heat-log`.**
  `profile --simulate --heat-log recorded.log` truncated the recorded log into
  5000 synthetic lookups and then analysed the synthetic data (`bench` did it
  without a word). The two flags are now mutually exclusive: `--simulate` writes
  the default `engram-tier.heat.log` and `--heat-log` reads a recording.
- **`serve` rejects a hotset that is not a profile report before printing a
  preview.** An arbitrary file produced `hotset: 0 keys ... sparse: false —` and
  only then failed with `cold tier init failed: tier: entry count must be
  positive, got 0`. The loader now asserts the precondition it actually needs
  (`distinct_keys > 0`) and says the file is not an engram-tier profile report
  and how to regenerate it.

### Changed

- **`serve` renders its sizes with the report's byte formatter.** The demo's
  64 KiB cold region read `0.0 GiB` one command after `profile` printed
  `64.0 KiB`; both now use the same formatter.

## [0.3.0] — 2026-10-21

### Fixed

- **A zero-lookup heat log no longer produces a confident nonsense report.**
  A header-only log (what a stale or truncated probe run leaves behind) was
  accepted by `profile`, which exited 0 claiming "Cold region (offload to
  SSD): 24.00 GiB" — the core offload estimate reported the whole table as
  cold when nothing was collected. `bench` swept and printed a full
  throughput-loss table for the same empty input. Both commands now fail
  loudly with "heat log … contains no lookups — the probe may not have run".
- **`--prompt` is forwarded as llama-cli's `-f` (prompt file), not `-p`
  (literal prompt).** llama.cpp defines `-p` as the prompt string and `-f` as
  "a file containing the prompt"; the v0.2.0 real-session path sent the file
  PATH to `-p`, silently profiling a workload whose prompt was the path text.
  The llama-cli argument construction is extracted into `llamaCliArgs` and
  unit-tested against the flag contract.
- **CLI errors are printed once, not twice.** Cobra printed "Error: X" and
  then `main.go` printed the returned error again, so every user-visible
  failure appeared twice on stderr. The root command now sets
  `SilenceErrors`, leaving `main.go` as the single error printer.

## [0.2.0] — 2026-09-15

### Fixed

- **llama.cpp probe retargeted at the real lookup site.** The v0.1.0 vendored
  patch had wrong hunk line counts in all three hunks (`git apply` rejected it
  with "corrupt patch at line 63"), contained C++ that could not compile (the
  call counter was used before its declaration; `std::uint64_t` without
  `<cstdint>`), and targeted `examples/speculative/llama-ngram-cache.h` — a
  file that never existed in any llama.cpp revision — so
  `engram-tier patch-llamacpp` always failed. The patch is now a
  mechanically-generated diff against `common/ngram-cache.cpp` at llama.cpp
  master `96ffdc41ceb055e1c2d3d96667ae6d9f0ccb710b` (pinned in the README,
  the methodology doc, and `--help`), emitting each 2–3-token n-gram key
  consulted by `common_ngram_cache_draft_partial` and its `try_draft`
  helpers. New tests validate the diff structurally, check it applies to a
  byte-exact vendored copy of the upstream file, and syntax-compile the
  extracted probe block.
- **`--simulate-alpha` no longer panics at the documented "uniform-ish"
  value.** `profile/bench --simulate --simulate-alpha 1.0` crashed with a raw
  `rand: nil Zipf` panic (math/rand/v2 defines no Zipf for s ≤ 1). Alpha ≤ 1
  now samples uniformly over the same rank space. Also fixed the rank-space
  off-by-one the new test caught: the Zipf path could produce
  `simulate-distinct + 1` distinct keys.
- **README quickstart works again.** `bash docs/demo.sh` (and the three Usage
  commands) referenced `examples/presentation.heat.log`, which was never
  committed; the fixture is now in the repo with the exact content recorded
  in `docs/demo-results.json`. The report no longer renders
  "top 33.3     %" (percent sign detached by width padding) or
  "Table size: 0.0 GiB" for a 64 KiB table; the demo record was regenerated
  from the actual v0.2.0 binary output.

### Added

- `engram-tier --version` (cobra's built-in flag, reporting
  `internal/cli.DefaultVersion`, overridable at link time for release
  builds).
- This changelog, a version-lockstep test (VERSION, `cli.DefaultVersion`,
  `web/site.json` `implementation_version`, `docs/demo-results.json`, and the
  `--version` output must agree), and the first tests for the `internal/cli`
  package.

## [0.1.0] — 2026-08-28

### Added

- n-gram access-heat profiler: heat-log collector, Pareto curve, working-set
  and cold-region sizing, sparseness verdict (`engram-tier profile`).
- Simulated Zipf workload generation (`--simulate`) and the coverage-sweep
  bench with a mathematical latency model (`engram-tier bench`).
- Cold-tier runtime components (mmap'd SSD region + LRU) composed by
  `engram-tier serve` as an m2 preview.
- llama.cpp instrumentation patch command (`engram-tier patch-llamacpp`),
  benchmark methodology doc, and the product site.

[Unreleased]: https://github.com/SuperMarioYL/engramtier/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/SuperMarioYL/engramtier/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/SuperMarioYL/engramtier/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.1.0
