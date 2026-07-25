#!/usr/bin/env bash
# Checkpoint + forced-restart resume proof (docs/PLAN.md Task 16 / SKP-14,
# Constitution C22): kill -9 the foundryd worker mid-plan, restart it, and
# prove the plan completes without re-doing task-1's already-finished work.
#
# Requires a live Postgres (migrations/0001-0003 applied) and a live
# Temporal reachable at TEMPORAL_HOSTPORT. There is no Docker daemon in
# this task's execution environment (the same established blocker as
# Tasks 2/3/4/8/12/13/14/15), so this script could not be run live here;
# it is provided as the real validation path for an environment that does
# have one (docs/PLAN.md §A "no self-reported done" — recorded honestly
# rather than faked). See docs/PLAN.md Task 16's Status line for exactly
# what was, and was not, executed.
#
# Two things this script deliberately does NOT invent:
#   - No CLI to start DeliverPlan against a live Temporal server exists
#     anywhere else in the repo (Task 12's own planned
#     `go run ./test/e2e/skp_basic` was never built either, per its
#     PLAN.md Status line) — test/helpers/startplan is the smallest
#     reversible addition to make this script runnable, kept under test/
#     rather than grown into cmd/foundry's production CLI surface.
#   - cmd/foundryd has no projector loop wired in yet despite Task 14's
#     card asking for one in foundryd (grep cmd/foundryd/main.go — it is
#     not there); this script calls `foundry projection rebuild`
#     explicitly instead, the same workaround test/status_consistency_e2e.sh
#     already uses.
#
# Security: the only process this script ever sends a signal to is the
# exact PID it itself started via $! immediately after exec'ing the
# foundryd binary it built — never a pattern-matched pkill/pgrep, which
# would risk killing an unrelated process on a shared machine.
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"
: "${TEMPORAL_HOSTPORT:=temporal:7233}"

WORKDIR="$(mktemp -d)"
FOUNDRYD_PID=""
cleanup() {
  if [ -n "${FOUNDRYD_PID}" ] && kill -0 "${FOUNDRYD_PID}" 2>/dev/null; then
    kill -9 "${FOUNDRYD_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

WF_ID="skp-resume-e2e-$$"
NEG_WF_ID="skp-resume-neg-$$"
PLAN_ID="plan-skp-resume-$$"
KEY_DIR="${WORKDIR}/keys"
WORKTREE_ROOT="${WORKDIR}/worktrees"
EVIDENCE_ROOT="${WORKDIR}/evidence"
REPO_PATH="${WORKDIR}/repo"
PLAN_FILE="${WORKDIR}/plan.md"
FOUNDRYD_BIN="${WORKDIR}/foundryd"

mkdir -p "${WORKTREE_ROOT}" "${EVIDENCE_ROOT}"

echo "== apply migrations =="
psql "${PG_DSN}" -f internal/db/migrations/00001_approved_plans.sql
psql "${PG_DSN}" -f internal/db/migrations/00002_transitions.sql
psql "${PG_DSN}" -f internal/db/migrations/00003_projection.sql

echo "== reset rows for this run's IDs =="
psql "${PG_DSN}" -c "DELETE FROM workflow_transitions WHERE workflow_id IN ('${WF_ID}', '${NEG_WF_ID}');"
psql "${PG_DSN}" -c "DELETE FROM workflow_status_projection WHERE workflow_id IN ('${WF_ID}', '${NEG_WF_ID}');"
psql "${PG_DSN}" -c "DELETE FROM receipts WHERE key LIKE '${WF_ID}|%' OR key LIKE '${NEG_WF_ID}|%';"

echo "== build a real git repo for worktree.Manager to branch off =="
git init -b main "${REPO_PATH}" >/dev/null
git -C "${REPO_PATH}" -c user.name=e2e -c user.email=e2e@example.com commit --allow-empty -m init >/dev/null

echo "== generate the two-task.md-shaped plan, adapted for the fake executor =="
# examples/plans/two-task.md's own goal fields are plain prose (real
# executors would run its commands); the fake executor repurposes Goal as
# a fake_script.yaml path (internal/executor/fake/doc.go), so this script
# builds its own copy of that same task shape — id/dependency chain
# unchanged from two-task.md — pointing each task's goal at a generated
# script instead. t1 is fast; t2 sleeps long enough to guarantee a kill
# window while it is still mid-flight.
T1_SCRIPT="${WORKDIR}/t1_script.yaml"
T2_SCRIPT="${WORKDIR}/t2_script.yaml"
cat > "${T1_SCRIPT}" <<'EOF'
patches:
  - path: greet.go
    content: "package greet\n"
claimed: "t1 done"
exit_code: 0
EOF
cat > "${T2_SCRIPT}" <<'EOF'
sleep_ms: 15000
patches:
  - path: greet_test.go
    content: "package greet\n"
claimed: "t2 done"
exit_code: 0
EOF

cat > "${PLAN_FILE}" <<EOF
---
id: ${PLAN_ID}
title: Two Task Dependency Chain (resume e2e)
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/two-task
    branch: main
tasks:
  - id: t1
    goal: ${T1_SCRIPT}
    commands:
      - noop
    validation_commands:
      - noop
    files:
      - greet.go
  - id: t2
    goal: ${T2_SCRIPT}
    depends_on:
      - t1
    commands:
      - noop
    validation_commands:
      - noop
    files:
      - greet_test.go
declared_effects:
  - kind: code
    target: greet.go
  - kind: code
    target: greet_test.go
requested_permissions:
  - kind: repo-write
    target: primary
budget_usd: 2.0
---
## Rationale

Adapted copy of examples/plans/two-task.md for the SKP-14 resume e2e
(docs/PLAN.md Task 16): same two-task dependency chain, goals repointed at
fake_script.yaml fixtures so the plan can run against the "fake" executor.
EOF

echo "== foundry keygen, plan submit, plan approve =="
go run ./cmd/foundry keygen --dir "${KEY_DIR}"
go run ./cmd/foundry plan submit --submitter alice "${PLAN_FILE}"
go run ./cmd/foundry plan approve \
  --submitter alice \
  --key-dir "${KEY_DIR}" \
  --pg-dsn "${PG_DSN}" \
  "${PLAN_FILE}"

echo "== build foundryd once, so kill -9 targets the exact binary this script started =="
go build -o "${FOUNDRYD_BIN}" ./cmd/foundryd

start_worker() {
  FOUNDRY_KEY_DIR="${KEY_DIR}" \
  FOUNDRY_WORKTREE_ROOT="${WORKTREE_ROOT}" \
  FOUNDRY_EVIDENCE_ROOT="${EVIDENCE_ROOT}" \
  PG_DSN="${PG_DSN}" \
  TEMPORAL_HOSTPORT="${TEMPORAL_HOSTPORT}" \
  "${FOUNDRYD_BIN}" &
  FOUNDRYD_PID=$!
  # Give the worker a moment to connect and start polling its task queue.
  sleep 2
}

echo "== start foundryd (first run) =="
start_worker
FIRST_PID="${FOUNDRYD_PID}"

echo "== start the plan workflow =="
go run ./test/helpers/startplan \
  --temporal-hostport "${TEMPORAL_HOSTPORT}" \
  --workflow-id "${WF_ID}" \
  --plan-id "${PLAN_ID}" \
  --plan-file "${PLAN_FILE}" \
  --repo-path "${REPO_PATH}" \
  --executor fake

echo "== wait for task-1's evidence bundle to appear =="
DEADLINE=$((SECONDS + 60))
T1_EVIDENCE_FOUND=0
while [ "${SECONDS}" -lt "${DEADLINE}" ]; do
  if grep -rl "\"TaskID\":\"t1\"" "${EVIDENCE_ROOT}" >/dev/null 2>&1; then
    T1_EVIDENCE_FOUND=1
    break
  fi
  sleep 1
done
if [ "${T1_EVIDENCE_FOUND}" -ne 1 ]; then
  echo "FAIL: task-1 evidence bundle never appeared under ${EVIDENCE_ROOT} within 60s" >&2
  exit 1
fi

echo "== kill -9 the exact foundryd PID this script started (${FIRST_PID}), mid t2 =="
kill -9 "${FIRST_PID}"
wait "${FIRST_PID}" 2>/dev/null || true

echo "== assert PG shows the workflow as RUNNING (stale is fine — that is the point) =="
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"
STATUS_DURING_KILL=$(psql "${PG_DSN}" -tA -c "SELECT status FROM workflow_status_projection WHERE workflow_id = '${WF_ID}';")
if [ "${STATUS_DURING_KILL}" != "RUNNING" ]; then
  echo "FAIL: workflow_status_projection.status = '${STATUS_DURING_KILL}', want RUNNING" >&2
  exit 1
fi

echo "== restart foundryd =="
start_worker

echo "== poll until the workflow reaches SUCCEEDED =="
DEADLINE=$((SECONDS + 90))
FINAL_STATUS=""
while [ "${SECONDS}" -lt "${DEADLINE}" ]; do
  FINAL_STATUS=$(go run ./cmd/foundry status "${WF_ID}" --fresh \
    --pg-dsn "${PG_DSN}" --temporal-hostport "${TEMPORAL_HOSTPORT}" \
    | grep '^status:' | awk '{print $2}') || true
  if [ "${FINAL_STATUS}" == "SUCCEEDED" ]; then
    break
  fi
  sleep 2
done
if [ "${FINAL_STATUS}" != "SUCCEEDED" ]; then
  echo "FAIL: workflow ${WF_ID} did not reach SUCCEEDED within 90s of restart (last seen: '${FINAL_STATUS}')" >&2
  exit 1
fi

echo "== assert task-1 was executed exactly once (receipts count for its ExecuteTask key) =="
T1_RECEIPT_COUNT=$(psql "${PG_DSN}" -tA -c "SELECT count(*) FROM receipts WHERE key = '${WF_ID}|t1|ExecuteTask|1';")
if [ "${T1_RECEIPT_COUNT}" != "1" ]; then
  echo "FAIL: receipts count for t1's ExecuteTask key = ${T1_RECEIPT_COUNT}, want exactly 1" >&2
  exit 1
fi

echo "skp_resume: OK — plan reached SUCCEEDED after a forced restart, task-1 executed exactly once"

# --- Negative control (Step 4): prove the receipt row, not luck or
# timing, is what enforces exactly-once. Run against real Postgres via
# test/helpers/execonce, which calls internal/kernel.Activities.ExecuteTask
# directly — no Temporal server involved — so the (workflow, task,
# attempt) key can be replayed a second time deterministically instead of
# needing to hit an exact kill-9 race window.
echo "== negative control: positive case (receipt intact) short-circuits a repeat call =="
NEG_WS="${WORKDIR}/neg-workspace"
go run ./test/helpers/execonce \
  --pg-dsn "${PG_DSN}" --workflow-id "${NEG_WF_ID}" --task-id t1 --attempt 1 \
  --script "${T1_SCRIPT}" --workspace "${NEG_WS}"

NEG_RECEIPT_COUNT=$(psql "${PG_DSN}" -tA -c "SELECT count(*) FROM receipts WHERE key = '${NEG_WF_ID}|t1|ExecuteTask|1';")
if [ "${NEG_RECEIPT_COUNT}" != "1" ]; then
  echo "FAIL: negative-control setup: expected exactly 1 receipt row before deleting it, got ${NEG_RECEIPT_COUNT}" >&2
  exit 1
fi

echo "== negative control: with the receipt intact, a repeat call to a now-missing script still succeeds (guard skipped the real re-run) =="
go run ./test/helpers/execonce \
  --pg-dsn "${PG_DSN}" --workflow-id "${NEG_WF_ID}" --task-id t1 --attempt 1 \
  --script "/no/such/script.yaml" --workspace "${NEG_WS}"

echo "== negative control: delete the receipt row, then prove the SAME call now genuinely re-runs and fails =="
psql "${PG_DSN}" -c "DELETE FROM receipts WHERE key = '${NEG_WF_ID}|t1|ExecuteTask|1';"
if go run ./test/helpers/execonce \
  --pg-dsn "${PG_DSN}" --workflow-id "${NEG_WF_ID}" --task-id t1 --attempt 1 \
  --script "/no/such/script.yaml" --workspace "${NEG_WS}"; then
  echo "FAIL: expected re-execution to fail loading the nonexistent script once its receipt row was deleted — the guard, not luck/timing, was what skipped this failure above" >&2
  exit 1
fi

echo "skp_resume negative control: OK — deleting the receipt row is what caused the real re-run"
