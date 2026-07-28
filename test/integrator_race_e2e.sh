#!/usr/bin/env bash
# Task 58 (TX-05) — integrator race e2e
# Verifies 3-way race linearizes; stale token rejected; receipts complete.
set -euo pipefail

go test ./internal/kernel/integrator/... -race -count=5 -v 2>&1 | grep -E "^--- (PASS|FAIL)|PASS|FAIL"

echo "integrator_race_e2e: all checks passed"
