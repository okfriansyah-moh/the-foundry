#!/usr/bin/env bash
# Task 51 (VEN-12) — improvement cycle e2e
# Happy path: in-envelope copy-tweak auto-admits (zero human touches).
# Halt path:  billing-touch fixture halts at H with notification.
set -euo pipefail

go test ./internal/mission/... -run "Improve|ImproveCycle" -race -count=1 -v 2>&1 | grep -E "^--- (PASS|FAIL)|PASS|FAIL"

echo "improvement_cycle_e2e: all checks passed"
