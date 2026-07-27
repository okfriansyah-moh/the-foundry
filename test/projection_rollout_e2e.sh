#!/usr/bin/env bash
# End-to-end versioned-projector rollout for internal/projection (docs/PLAN.md
# Task 38 / FND-19 Acceptance: "a live rollout during running workflows loses
# zero updates" — tested with a generator load).
#
# Requires a live Postgres reachable at PG_DSN with migrations 00002, 00003,
# and 00011 applied. Mirrors test/projection_rebuild_e2e.sh's structure
# (same established convention: docker/postgres availability varies by
# environment — see that script's doc comment).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"

echo "== apply migrations (via foundry migrate, not psql -f: each migration file"
echo "   carries both a +goose Up and +goose Down section -- psql -f would run"
echo "   both back to back and undo its own CREATE TABLE) =="
PG_DSN="${PG_DSN}" go run ./cmd/foundry migrate up

echo "== reset tables =="
psql "${PG_DSN}" -c "TRUNCATE workflow_transitions, workflow_status_projection, workflow_status_projection_shadow;"
psql "${PG_DSN}" -c "DELETE FROM projection_offsets;"

echo "== seed 10 workflows and project them (this is 'before the rollout') =="
for i in $(seq 1 10); do
  psql "${PG_DSN}" -c "INSERT INTO workflow_transitions (workflow_id, payload) VALUES ('rollout-e2e-wf-${i}', '{\"WorkflowID\":\"rollout-e2e-wf-${i}\",\"Status\":\"RUNNING\",\"PhaseTo\":\"executing\",\"Attempt\":1,\"OccurredAt\":\"2026-07-25T00:00:00Z\"}');"
done
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"

echo "== start generator load: append transitions concurrently with the rollout (simulates running workflows) =="
(
  for i in $(seq 1 30); do
    psql "${PG_DSN}" -c "INSERT INTO workflow_transitions (workflow_id, payload) VALUES ('rollout-e2e-gen-${i}', '{\"WorkflowID\":\"rollout-e2e-gen-${i}\",\"Status\":\"RUNNING\",\"PhaseTo\":\"executing\",\"Attempt\":1,\"OccurredAt\":\"2026-07-25T00:01:00Z\"}');" >/dev/null
    sleep 0.05
  done
) &
GENERATOR_PID=$!

echo "== foundry projection rollout --to-version=v1 (racing the generator) =="
go run ./cmd/foundry projection rollout --to-version=v1 --pg-dsn "${PG_DSN}"

wait "${GENERATOR_PID}"
echo "== generator done =="

echo "== drain the live projector once more (represents the next scheduled tick/rebuild) =="
go run ./cmd/foundry projection rebuild --pg-dsn "${PG_DSN}"

echo "== assert zero updates lost: every workflow's projected last_seq matches its latest transition =="
MISMATCHES=$(psql "${PG_DSN}" -tA -c "
  SELECT count(*)
  FROM workflow_status_projection p
  JOIN (SELECT workflow_id, max(seq) AS max_seq FROM workflow_transitions GROUP BY workflow_id) t
    ON t.workflow_id = p.workflow_id
  WHERE p.last_seq <> t.max_seq;
")
if [ "${MISMATCHES}" != "0" ]; then
  echo "FAIL: ${MISMATCHES} workflow(s) did not reflect their latest transition after rollout — an update was lost" >&2
  exit 1
fi

EXPECTED=$(psql "${PG_DSN}" -tA -c "SELECT count(DISTINCT workflow_id) FROM workflow_transitions;")
ACTUAL=$(psql "${PG_DSN}" -tA -c "SELECT count(*) FROM workflow_status_projection;")
if [ "${EXPECTED}" != "${ACTUAL}" ]; then
  echo "FAIL: projected row count = ${ACTUAL}, want ${EXPECTED} distinct workflows" >&2
  exit 1
fi

echo "projection_rollout_e2e: OK (${ACTUAL} workflows, zero updates lost across a live rollout)"
