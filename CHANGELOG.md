# Changelog

All notable changes to EngramTier are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/SuperMarioYL/engramtier/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/engramtier/releases/tag/v0.1.0
