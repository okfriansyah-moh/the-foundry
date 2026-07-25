#!/usr/bin/env bash
# Induced-lag round trip for `foundry status` (docs/PLAN.md Task 15 / SKP-13
# Acceptance: "during induced lag, projected != fresh detected by test; after
# resume, equal").
#
# Requires a live Postgres (migrations/0002, 0003 applied) and a live
# Temporal reachable at TEMPORAL_HOSTPORT, plus the kernel workflow actually
# running the target workflow ID so DescribeWorkflowExecution resolves. There
# is no Docker daemon in this task's execution environment (the same
# established blocker as Tasks 2/4/8/12/13/14), so this script could not be
# run live here; it is provided as the real validation path for an
# environment that does have one (docs/PLAN.md §A "no self-reported done" —
# recorded honestly rather than faked).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"
: "${TEMPORAL_HOSTPORT:=temporal:7233}"
WF_ID="status-consistency-e2e-$$"

echo "== apply migrations =="
psql "${PG_DSN}" -f internal/db/migrations/00002_transitions.sql
psql "${PG_DSN}" -f internal/db/migrations/00003_projection.sql

echo "== reset tables for ${WF_ID} =="
psql "${PG_DSN}" -c "DELETE FROM workflow_transitions WHERE workflow_id = '${WF_ID}';"
psql "${PG_DSN}" -c "DELETE FROM workflow_status_projection WHERE workflow_id = '${WF_ID}';"

echo "== seed transition 1 (RUNNING/acquiring-worktree), then project it =="
psql "${PG_DSN}" <<SQL
INSERT INTO workflow_transitions (workflow_id, payload) VALUES
  ('${WF_ID}', '{"WorkflowID":"${WF_ID}","Status":"RUNNING","PhaseTo":"acquiring-worktree","Attempt":1,"OccurredAt":"2026-07-21T00:00:00Z"}');
SQL
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"

echo "== induce lag: append transition 2 WITHOUT ticking the projector =="
psql "${PG_DSN}" <<SQL
INSERT INTO workflow_transitions (workflow_id, payload) VALUES
  ('${WF_ID}', '{"WorkflowID":"${WF_ID}","Status":"RUNNING","PhaseTo":"executing","Attempt":1,"OccurredAt":"2026-07-21T00:01:00Z"}');
SQL

echo "== projected status should still show the stale phase =="
PROJECTED_PHASE=$(go run ./cmd/foundry status "${WF_ID}" --pg-dsn "${PG_DSN}" | grep '^phase:' | awk '{print $2}')
echo "== fresh status should show the current phase read directly off workflow_transitions =="
FRESH_PHASE=$(go run ./cmd/foundry status "${WF_ID}" --fresh --pg-dsn "${PG_DSN}" --temporal-hostport "${TEMPORAL_HOSTPORT}" | grep '^phase:' | awk '{print $2}')

if [ "${PROJECTED_PHASE}" == "${FRESH_PHASE}" ]; then
  echo "FAIL: expected projected ('${PROJECTED_PHASE}') != fresh ('${FRESH_PHASE}') during induced lag" >&2
  exit 1
fi
if [ "${FRESH_PHASE}" != "executing" ]; then
  echo "FAIL: fresh phase = '${FRESH_PHASE}', want 'executing'" >&2
  exit 1
fi

echo "== resume the projector (tick once) =="
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"

echo "== projected and fresh should now converge =="
PROJECTED_PHASE_2=$(go run ./cmd/foundry status "${WF_ID}" --pg-dsn "${PG_DSN}" | grep '^phase:' | awk '{print $2}')
FRESH_PHASE_2=$(go run ./cmd/foundry status "${WF_ID}" --fresh --pg-dsn "${PG_DSN}" --temporal-hostport "${TEMPORAL_HOSTPORT}" | grep '^phase:' | awk '{print $2}')

if [ "${PROJECTED_PHASE_2}" != "${FRESH_PHASE_2}" ]; then
  echo "FAIL: projected ('${PROJECTED_PHASE_2}') and fresh ('${FRESH_PHASE_2}') did not converge after resume" >&2
  exit 1
fi

echo "status_consistency_e2e: OK (converged on phase '${FRESH_PHASE_2}')"
