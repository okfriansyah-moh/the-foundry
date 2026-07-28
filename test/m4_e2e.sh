#!/usr/bin/env bash
# docs/PLAN.md Task 93 (PRV-10) — M4 milestone-exit e2e.
#
# Mirrors the shape of the other milestone-exit cards (Tasks 19/39/53/63/73):
# it proves the milestone's own exit criteria end-to-end and archives the
# evidence. Unlike the live-service e2es, this one needs no Temporal/Postgres:
# executor selection is an ordinary in-process kernel decision, so the harness
# (test/helpers/m4harness) drives internal/kernel.Activities.ExecuteTask
# directly against the REAL capability registry, REAL routing table, and REAL
# selector, with gated-stub provider binaries (no API spend).
#
# Asserts (harness exits nonzero on any failure):
#   - 3 explicit-executor tasks (claude-code, opencode, gemini-cli) selected
#     inside internal/kernel, each recording ExecutorUsed on its bundle;
#   - 1 routed-default task resolved via config/executor-routing.yaml;
#   - 1 denied executor fails closed with the exact policy-violation class;
#   - PhaseHint present when set, absent when empty;
#   - capability-registry staleness lint clean.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "== m4 e2e: driving real kernel selector + capability registry + routing =="
go run ./test/helpers/m4harness/

echo "== m4 e2e: capability-registry staleness lint (Task 84) =="
fitlint_bin="$(mktemp -d)/fitlint"
go build -o "${fitlint_bin}" ./cmd/fitlint
"${fitlint_bin}" capability config/executor-capabilities.yaml

echo "== m4 e2e: evidence archive =="
ls -R evidence/m4-exit/ | head -40
test -f evidence/m4-exit/SUMMARY.txt

echo "m4 e2e OK"
