#!/usr/bin/env bash
set -euo pipefail
go test ./internal/opportunity/... ./internal/kernel/ -count=1 -run 'Signal|Opportunity|Real' 2>&1 | grep -E "ok|FAIL|PASS"
go test ./test/e2e/validation_signal/... -count=1 2>&1 | grep -E "ok|FAIL|PASS"
echo "==> validation_signal e2e: PASS"
