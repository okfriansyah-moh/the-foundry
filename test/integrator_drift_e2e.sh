#!/usr/bin/env bash
# Task 59 (TX-06) — integrator drift guard e2e
# Verifies: clean rebase → requeued; conflicting rebase → PROVEN_BLOCKED.
set -euo pipefail

go test ./internal/kernel/integrator/... -run "EvaluateDrift" -race -count=1 -v 2>&1 | grep -E "^--- (PASS|FAIL)|PASS|FAIL"

echo "integrator_drift_e2e: all checks passed"
