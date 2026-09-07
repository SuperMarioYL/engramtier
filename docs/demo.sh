#!/usr/bin/env bash
set -euo pipefail
go run ./cmd/engram-tier profile --heat-log examples/presentation.heat.log --table-bytes 64KiB --entry-bytes 16 --coverage 0.9
