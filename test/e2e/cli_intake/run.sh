#!/usr/bin/env bash
set -euo pipefail
go test ./internal/intake/... ./test/e2e/cli_intake/... -count=1 2>&1 | grep -E "ok|FAIL|PASS"
echo "==> cli_intake e2e: PASS"
