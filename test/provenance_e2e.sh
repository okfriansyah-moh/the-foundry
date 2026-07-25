#!/usr/bin/env bash
# End-to-end CLI round trip for internal/provenance (docs/PLAN.md Task 8 /
# SKP-06 Acceptance: "CLI round-trip green in e2e script").
#
# Requires a live Postgres reachable at PG_DSN (or via the "postgres"
# compose service — see deploy/docker-compose.yaml) with
# internal/db/migrations/00001_approved_plans.sql applied. There is no Docker daemon in
# this task's execution environment, so this script could not be run live
# here; it is provided as the real validation path for an environment that
# does have one (docs/PLAN.md §A "no self-reported done" — recorded
# honestly rather than faked).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

KEY_DIR="${WORKDIR}/keys"
PLAN_FILE="${WORKDIR}/plan.md"

cat > "${PLAN_FILE}" <<'EOF'
---
id: plan-e2e-provenance
title: Provenance e2e fixture
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/fixture
    branch: main
tasks:
  - id: t1
    goal: Fixture task
    commands:
      - echo noop
    validation_commands:
      - echo ok
declared_effects:
  - kind: docs
    target: README.md
requested_permissions:
  - kind: repo-read
    target: "*"
budget_usd: 1.0
---
## Rationale

Fixture for the provenance e2e round trip.
EOF

echo "== apply migration =="
psql "${PG_DSN}" -f internal/db/migrations/00001_approved_plans.sql

echo "== foundry keygen =="
go run ./cmd/foundry keygen --dir "${KEY_DIR}"

echo "== foundry plan submit =="
go run ./cmd/foundry plan submit --submitter alice "${PLAN_FILE}"

echo "== foundry plan approve =="
go run ./cmd/foundry plan approve \
  --submitter alice \
  --key-dir "${KEY_DIR}" \
  --pg-dsn "${PG_DSN}" \
  "${PLAN_FILE}"

echo "== foundry plan verify =="
go run ./cmd/foundry plan verify \
  --file "${PLAN_FILE}" \
  --key-dir "${KEY_DIR}" \
  --pg-dsn "${PG_DSN}" \
  plan-e2e-provenance

echo "== tamper the plan file and confirm verify fails =="
sed -i.bak 's/Fixture task/Tampered task/' "${PLAN_FILE}"
if go run ./cmd/foundry plan verify \
  --file "${PLAN_FILE}" \
  --key-dir "${KEY_DIR}" \
  --pg-dsn "${PG_DSN}" \
  plan-e2e-provenance; then
  echo "FAIL: expected plan verify to reject a tampered plan file" >&2
  exit 1
fi

echo "provenance e2e: OK"
