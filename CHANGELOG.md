# Changelog

All notable changes to EngramTier are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/SuperMarioYL/engramtier/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/SuperMarioYL/engramtier/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.1.0
