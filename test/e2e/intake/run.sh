#!/usr/bin/env bash
# Task 111 (INT-03) — idea-intake pipeline e2e (cassette, zero network).
#
# Proves the staged pipeline end to end on fixtures and a spec cassette with no
# network and no database:
#   idea → opportunity Score/Decide (fixture) → verdict gate → spec synthesis
#   (ReplaySource cassette) → PLAN generation → admission → approval → mission
#   start (recording starter).
#
# Exit criteria per Task 111 Acceptance:
#   - a BUILD idea reaches a running mission with zero further human input
#     (auto-approved personal-autonomous profile);
#   - a REJECT verdict ends the run having created no repository, no plan
#     approval and no build reservation;
#   - resuming an interrupted run produces the same final artifacts with no
#     duplicated provider call or budget charge.
#
# CI mode uses fixtures/cassettes only. A live intake run substitutes the
# research intake path and a Temporal-backed starter.

set -euo pipefail

echo "==> [1] Intake pipeline unit contract (branches, idempotency, fail-closed budget)"
go test ./internal/intake/... -race -count=1 2>&1 | grep -E "ok|FAIL"

echo "==> [2] Intake e2e on real adapters + cassettes (happy/reject/resume)"
go test ./test/e2e/intake/... -race -count=1 -v 2>&1 | grep -E "RUN|PASS|FAIL|ok "

echo "==> intake e2e: PASS"
