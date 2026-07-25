#!/usr/bin/env bash
# End-to-end rebuild round trip for internal/projection (docs/PLAN.md Task 14
# / SKP-12 Acceptance: "drop table -> rebuild -> identical checksum;
# out-of-order/duplicate seq handled idempotently").
#
# Requires a live Postgres reachable at PG_DSN with migrations/0002 and
# migrations/0003 applied. There is no Docker daemon in this task's
# execution environment (the same established blocker as Tasks 2/4/8/12/13),
# so this script could not be run live here; it is provided as the real
# validation path for an environment that does have one (docs/PLAN.md §A
# "no self-reported done" — recorded honestly rather than faked).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"

echo "== apply migrations =="
psql "${PG_DSN}" -f internal/db/migrations/00002_transitions.sql
psql "${PG_DSN}" -f internal/db/migrations/00003_projection.sql

echo "== reset tables =="
psql "${PG_DSN}" -c "TRUNCATE workflow_transitions, workflow_status_projection;"
psql "${PG_DSN}" -c "DELETE FROM projection_offsets;"

echo "== seed transitions, including an out-of-order/duplicate delivery for wf-a =="
psql "${PG_DSN}" <<'SQL'
INSERT INTO workflow_transitions (workflow_id, payload) VALUES
  ('wf-a', '{"WorkflowID":"wf-a","Status":"RUNNING","PhaseTo":"acquiring-worktree","Attempt":1,"OccurredAt":"2026-07-21T00:00:00Z"}'),
  ('wf-a', '{"WorkflowID":"wf-a","Status":"RUNNING","PhaseTo":"executing","Attempt":1,"OccurredAt":"2026-07-21T00:01:00Z"}'),
  ('wf-b', '{"WorkflowID":"wf-b","Status":"SUCCEEDED","PhaseTo":"done","Attempt":1,"OccurredAt":"2026-07-21T00:02:00Z"}'),
  -- duplicate/out-of-order redelivery of wf-a's first (stale) transition,
  -- appended last (highest seq) but carrying the *older* phase — the
  -- idempotent-upsert guard must NOT let this regress wf-a's projected row.
  ('wf-a', '{"WorkflowID":"wf-a","Status":"RUNNING","PhaseTo":"acquiring-worktree","Attempt":1,"OccurredAt":"2026-07-21T00:00:00Z"}');
SQL

echo "== foundry projection rebuild (first pass) =="
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"
CHECKSUM_1=$(psql "${PG_DSN}" -tA -c "SELECT projection_checksum();")

echo "== assert wf-a projected to its LATEST transition, not the stale redelivery =="
PHASE_A=$(psql "${PG_DSN}" -tA -c "SELECT phase FROM workflow_status_projection WHERE workflow_id = 'wf-a';")
if [ "${PHASE_A}" != "executing" ]; then
  echo "FAIL: wf-a projected phase = '${PHASE_A}', want 'executing' (idempotent upsert guard regressed)" >&2
  exit 1
fi

echo "== capture pre-drop checksum, drop projection table contents, rebuild =="
CHECKSUM_BEFORE_DROP=$(psql "${PG_DSN}" -tA -c "SELECT projection_checksum();")
psql "${PG_DSN}" -c "TRUNCATE workflow_status_projection;"
psql "${PG_DSN}" -c "DELETE FROM projection_offsets;"

echo "== foundry projection rebuild (post-drop pass) =="
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"
CHECKSUM_AFTER_REBUILD=$(psql "${PG_DSN}" -tA -c "SELECT projection_checksum();")

if [ "${CHECKSUM_BEFORE_DROP}" != "${CHECKSUM_AFTER_REBUILD}" ]; then
  echo "FAIL: checksum before drop (${CHECKSUM_BEFORE_DROP}) != checksum after rebuild (${CHECKSUM_AFTER_REBUILD})" >&2
  exit 1
fi
if [ "${CHECKSUM_1}" != "${CHECKSUM_AFTER_REBUILD}" ]; then
  echo "FAIL: rebuild is not reproducible across repeated runs" >&2
  exit 1
fi

echo "projection_rebuild_e2e: OK (checksum ${CHECKSUM_AFTER_REBUILD})"
