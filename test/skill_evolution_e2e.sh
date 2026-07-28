#!/usr/bin/env bash
# docs/PLAN.md Task 77 (EVO-04) — L1 capability-evolution both-path e2e.
#
# Proves against the real internal/evolve L1 pipeline (no Temporal needed —
# the pipeline is deterministic Go): a personal prompt improvement flows to
# promotion; a permission-expanding candidate is rejected at the L1 gate; an
# org candidate is proposal-only.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== skill-evolution e2e: driving real internal/evolve L1 pipeline =="
go run ./test/helpers/skillevolve/

echo "== skill-evolution e2e: unit condition-gate matrix =="
go test ./internal/evolve/ -run 'L1' -race

echo "skill-evolution e2e OK"
