#!/usr/bin/env bash
set -euo pipefail
go test ./internal/mission/... ./test/e2e/venture_improvement/... -count=1
echo "==> venture_improvement: PASS (unit/workflow); live deploy gated"
