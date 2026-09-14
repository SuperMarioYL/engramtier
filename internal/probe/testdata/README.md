// Vendored upstream fixture for the llama.cpp probe patch apply test.
//
// This file is a byte-exact copy of llama.cpp's common/ngram-cache.cpp at
// master commit 96ffdc41ceb055e1c2d3d96667ae6d9f0ccb710b (verified
// 2026-09-14). It must NOT be edited: TestVendoredPatchAppliesToFixture
// checks internal/probe/llamacpp-ngram.patch against it with
// `git apply --check`, so any upstream drift that breaks the vendored patch
// fails `go test` until the patch is regenerated.
//
// Source: https://github.com/ggml-org/llama.cpp (MIT license,
// https://github.com/ggml-org/llama.cpp/blob/master/LICENSE). Vendored here
// for testing under the same MIT terms as this repository.
