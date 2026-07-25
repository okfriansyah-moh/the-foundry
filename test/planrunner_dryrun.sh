#!/usr/bin/env bash
# Scripted dry run for tools/planrunner (Task 3 Validation): drives a scratch copy of a
# fixture plan through the three required scenarios in Acceptance — one AUTO completion,
# one GATED pause+approve, and one halted-on-failure case — without touching this repo's
# real git state, docs/PLAN.md, or a live Telegram bot (dryrun mode uses in-memory fakes).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fixture="tools/planrunner/testdata/fixture_plan.md"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "== dry run: AUTO completion (task 2, Low/R1) =="
cp "$fixture" "$work/auto.md"
out_auto="$(go run ./tools/planrunner --mode=dryrun --plan="$work/auto.md" --only-task=2)"
echo "$out_auto"
echo "$out_auto" | grep -q "^AUTO_COMPLETED task=2" || { echo "FAIL: expected AUTO_COMPLETED for task 2"; exit 1; }
grep -q '\*\*Status:\*\* ✅' "$work/auto.md" || { echo "FAIL: task 2 Status line not flipped to done"; exit 1; }

echo
echo "== dry run: GATED pause + approve (task 3, High/R3) =="
cp "$fixture" "$work/gated.md"
out_gated="$(go run ./tools/planrunner --mode=dryrun --plan="$work/gated.md" --only-task=3 --auto-approve=true)"
echo "$out_gated"
echo "$out_gated" | grep -q "^GATED_PENDING task=3" || { echo "FAIL: expected GATED_PENDING for task 3"; exit 1; }
echo "$out_gated" | grep -q "^GATED_APPROVED task=3" || { echo "FAIL: expected GATED_APPROVED for task 3"; exit 1; }

echo
echo "== dry run: halted after two consecutive validation failures (task 4) =="
cp "$fixture" "$work/halt.md"
set +e
out_halt="$(go run ./tools/planrunner --mode=dryrun --plan="$work/halt.md" --only-task=4 --fail-task=4)"
halt_status=$?
set -e
echo "$out_halt"
[ "$halt_status" -ne 0 ] || { echo "FAIL: expected non-zero exit on halt"; exit 1; }
echo "$out_halt" | grep -q "^HALT_ALERT task=4" || { echo "FAIL: expected HALT_ALERT for task 4"; exit 1; }
echo "$out_halt" | grep -q "^HALTED task=4" || { echo "FAIL: expected HALTED outcome for task 4"; exit 1; }
grep -q '\*\*Status:\*\* ☐ Not started' "$work/halt.md" || { echo "FAIL: a halted task must not be marked done"; exit 1; }

echo
echo "dry run OK: AUTO completion, GATED approve, and halt-on-failure all verified"
