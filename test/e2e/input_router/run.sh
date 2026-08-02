#!/usr/bin/env bash
set -euo pipefail
go test ./internal/inputrouter/... ./test/e2e/input_router/... -count=1
echo "==> input_router: PASS"
