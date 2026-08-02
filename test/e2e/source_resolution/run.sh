#!/usr/bin/env bash
set -euo pipefail
echo "==> repository registry + resolver"
go test ./internal/repository/... ./test/e2e/source_resolution/... -count=1 2>&1 | grep -E "ok|FAIL|PASS"
echo "==> source_resolution e2e: PASS"
