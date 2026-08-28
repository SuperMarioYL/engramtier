#!/usr/bin/env bash
#
# Rebuild llama.cpp with the EngramTier n-gram access-heat probe applied.
# Invoked by `engram-tier patch-llamacpp --rebuild` after the patch is applied;
# safe to call directly as `scripts/build-llamacpp.sh <llamacpp-root>`.
#
# The probe is env-gated (ENGRAM_TIER_PROFILE), so this produces a single
# binary that serves both profile and serve modes — no separate instrumented
# build is needed. We build the speculative-decoding example, which is the
# lookup site the patch instruments, plus the main CLI.
set -euo pipefail

LLAMACPP_ROOT="${1:-${LLAMACPP_ROOT:-}}"
if [[ -z "$LLAMACPP_ROOT" ]]; then
  echo "usage: $0 <path-to-llama.cpp-checkout>" >&2
  exit 2
fi
if [[ ! -d "$LLAMACPP_ROOT" ]]; then
  echo "build-llamacpp: llama.cpp root not found: $LLAMACPP_ROOT" >&2
  exit 1
fi

BUILD_DIR="${LLAMACPP_ROOT}/build-engram-tier"
mkdir -p "$BUILD_DIR"
cd "$BUILD_DIR"

# CMake config: Apple Silicon uses Metal, but the probe is engine-agnostic —
# it instruments the n-gram lookup regardless of the matmul backend. We keep
# defaults so the user's existing llama.cpp build flags win on re-config.
echo "==> cmake configure"
cmake "$LLAMACPP_ROOT" \
  -DCMAKE_BUILD_TYPE=Release \
  -DLLAMA_CURL=OFF

echo "==> cmake build (speculative example + main)"
cmake --build . --config Release -- -j"$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 8)" \
  --target llama-speculative llama-cli

BIN="$BUILD_DIR/bin"
echo
echo "EngramTier instrumented llama.cpp built."
echo "  speculative (n-gram lookup probe): $BIN/llama-speculative"
echo "  main CLI:                          $BIN/llama-cli"
echo
echo "Next: ENGRAM_TIER_PROFILE=1 ENGRAM_TIER_HEAT_LOG=engram-tier.heat.log \\"
echo "  $BIN/llama-speculative -m <qwen3.8-flash-next-q4.gguf> -n 512"
echo "  engram-tier profile --heat-log engram-tier.heat.log --table-bytes 24GiB"
