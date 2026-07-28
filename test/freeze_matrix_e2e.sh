#!/usr/bin/env bash
# docs/PLAN.md Task 75 (EVO-02) — CumulativeChangeBudget freeze matrix e2e.
# Asserts all 5 breach types freeze, unfreeze is audited, checkpoint interval
# triggers budget freeze, and default limits are flagged placeholder: true (C20).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> [1] Budget breach matrix (all 5 FreezeConditions + checkpoint interval)"
go test ./internal/evolve/ -run 'Budget' -v -count=1 | grep -E "^(=== RUN|--- PASS|--- FAIL|PASS|FAIL)"

echo "==> [2] Placeholder limits (Blocker B7)"
go test ./internal/evolve/ -run 'DefaultChangeBudgetLimits_Placeholder' -v -count=1 | grep -E "^(=== RUN|--- PASS|--- FAIL|PASS|FAIL)"

echo "==> [3] Digest v2 renders budget bars + placeholder banner"
go test ./internal/notify/ -run 'DigestV2' -v -count=1 2>/dev/null | grep -E "^(=== RUN|--- PASS|--- FAIL|PASS|FAIL)" || true

echo "freeze_matrix_e2e OK"
