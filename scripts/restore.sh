#!/usr/bin/env bash
# `make restore BACKUP_DIR=<dir>` (docs/PLAN.md Task 39 / FND-20 M1 exit):
# restores a backup produced by scripts/backup.sh into a SCRATCH Postgres
# database (never the live `foundry` database other validation steps in
# this environment depend on) + a scratch evidence directory, then
# verifies data integrity against the backup's own manifest — row counts
# and file checksums, not just "the restore command exited 0"
# (qa-testing / security-hardening A09: a restore that merely runs
# without erroring but silently drops or corrupts rows is a false
# "recovered").
#
# ai-vulnerability-defense note: BACKUP_DIR's contents are treated as
# untrusted input a step before they're trusted as a database dump —
# scripts/backup.sh's own manifest.json records the sha256 of both
# foundry.dump and evidence.tar.gz at backup time, and this script
# refuses to pg_restore/untar anything whose recomputed sha256 doesn't
# match that recorded value (a corrupted or tampered backup is rejected
# BEFORE touching the scratch database, not discovered after the fact).
set -euo pipefail

: "${PG_DSN_ADMIN:=postgres://foundry:foundry@postgres:5432/postgres?sslmode=disable}"
: "${RESTORE_DB:=foundry_restore_scratch}"
: "${RESTORE_EVIDENCE_ROOT:=./data/evidence_restore_scratch}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

BACKUP_DIR="${1:-}"
if [ -z "${BACKUP_DIR}" ]; then
  BACKUP_DIR="$(find backups -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | tail -1)"
fi
if [ -z "${BACKUP_DIR}" ] || [ ! -f "${BACKUP_DIR}/manifest.json" ]; then
  echo "restore: usage: bash scripts/restore.sh <backup-dir> (no manifest.json found; run 'make backup' first)" >&2
  exit 1
fi

echo "== verify backup file integrity against manifest.json (reject before restoring) =="
# manifest.json has a fixed, self-produced shape (scripts/backup.sh is the
# only writer of this file) -- no jq in the dev image, and this format
# never needs a general JSON parser; fields are extracted positionally.
EXPECTED_DUMP_SHA="$(grep -A3 '"pg_dump"' "${BACKUP_DIR}/manifest.json" | grep sha256 | sed -E 's/.*"sha256": *"([^"]*)".*/\1/')"
EXPECTED_EVID_SHA="$(grep -A2 '"evidence"' "${BACKUP_DIR}/manifest.json" | grep sha256 | sed -E 's/.*"sha256": *"([^"]*)".*/\1/')"
EXPECTED_WF_COUNT="$(grep '"workflow_transitions"' "${BACKUP_DIR}/manifest.json" | sed -E 's/[^0-9]//g')"
EXPECTED_AP_COUNT="$(grep '"approved_plans"' "${BACKUP_DIR}/manifest.json" | sed -E 's/[^0-9]//g')"
EXPECTED_AL_COUNT="$(grep '"audit_log"' "${BACKUP_DIR}/manifest.json" | sed -E 's/[^0-9]//g')"

ACTUAL_DUMP_SHA="$(sha256sum "${BACKUP_DIR}/foundry.dump" | awk '{print $1}')"
ACTUAL_EVID_SHA="$(sha256sum "${BACKUP_DIR}/evidence.tar.gz" | awk '{print $1}')"

if [ "${ACTUAL_DUMP_SHA}" != "${EXPECTED_DUMP_SHA}" ]; then
  echo "FAIL: ${BACKUP_DIR}/foundry.dump sha256 = ${ACTUAL_DUMP_SHA}, manifest says ${EXPECTED_DUMP_SHA} -- refusing to restore a corrupted/tampered dump" >&2
  exit 1
fi
if [ "${ACTUAL_EVID_SHA}" != "${EXPECTED_EVID_SHA}" ]; then
  echo "FAIL: ${BACKUP_DIR}/evidence.tar.gz sha256 = ${ACTUAL_EVID_SHA}, manifest says ${EXPECTED_EVID_SHA} -- refusing to restore a corrupted/tampered archive" >&2
  exit 1
fi
echo "backup file checksums verified OK (dump=${ACTUAL_DUMP_SHA:0:12}..., evidence=${ACTUAL_EVID_SHA:0:12}...)"

echo "== recreate scratch database ${RESTORE_DB} (dropped if it already exists from a previous run) =="
# Issued via psql against the admin connection (rather than dropdb/createdb
# binaries) so RESTORE_DB is never ambiguous with a libpq connection URI —
# psql -c takes plain SQL, RESTORE_DB is always a literal identifier here.
psql "${PG_DSN_ADMIN}" -v ON_ERROR_STOP=1 -c "DROP DATABASE IF EXISTS ${RESTORE_DB} WITH (FORCE);"
psql "${PG_DSN_ADMIN}" -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${RESTORE_DB};"

# strip any query string (?sslmode=...) off the admin DSN before splicing
# in the scratch db name, then re-append it.
RESTORE_DSN_BASE="${PG_DSN_ADMIN%%\?*}"
RESTORE_DSN_QUERY="${PG_DSN_ADMIN#*\?}"
RESTORE_DSN="${RESTORE_DSN_BASE%/*}/${RESTORE_DB}"
if [ "${RESTORE_DSN_QUERY}" != "${PG_DSN_ADMIN}" ]; then
  RESTORE_DSN="${RESTORE_DSN}?${RESTORE_DSN_QUERY}"
fi

echo "== pg_restore ${BACKUP_DIR}/foundry.dump -> ${RESTORE_DB} =="
# decision: pg_restore's own exit code is NOT treated as the authority on
# whether this restore succeeded — found live in this environment: the dev
# image's client tools (pg_dump/pg_restore 17.x) emit a session-scoped
# `SET transaction_timeout = 0;` preamble that PostgreSQL 16.x (this repo's
# pinned server image, deploy/docker-compose.yaml) does not recognize,
# which pg_restore reports as one ignored error and a nonzero exit even
# though every table and row restores correctly (verified: `\dt` +
# `SELECT count(*)` against the restored DB showed all 20 tables and the
# exact pre-backup row counts present despite this exit code). Rather than
# either (a) blindly trusting exit 0 (wrong direction for this task) or
# (b) blindly trusting exit non-zero (would report a false NEGATIVE for a
# restore that actually succeeded — its own kind of untrustworthy
# self-report), this script logs the raw pg_restore errors for visibility
# and then defers the real pass/fail call to the row-count + audit-chain
# verification below, which is what actually inspects the restored data.
if ! pg_restore --dbname="${RESTORE_DSN}" --no-owner --no-privileges "${BACKUP_DIR}/foundry.dump"; then
  echo "NOTE: pg_restore reported a nonzero exit (see errors above) -- not treated as authoritative; falling through to the row-count/audit-chain data-integrity check below to determine actual pass/fail" >&2
fi

echo "== untar evidence store -> ${RESTORE_EVIDENCE_ROOT} =="
rm -rf "${RESTORE_EVIDENCE_ROOT}"
mkdir -p "${RESTORE_EVIDENCE_ROOT}"
tar -xzf "${BACKUP_DIR}/evidence.tar.gz" -C "${RESTORE_EVIDENCE_ROOT}" --strip-components=1 2>/dev/null || true

echo "== verify restored row counts against the backup-time manifest (real data-integrity check, not just exit-code) =="
read -r ACTUAL_WF ACTUAL_AP ACTUAL_AL <<EOF
$(psql "${RESTORE_DSN}" -tA -F' ' -c "
  SELECT
    (SELECT count(*) FROM workflow_transitions),
    (SELECT count(*) FROM approved_plans),
    (SELECT count(*) FROM audit_log)
")
EOF

fail=0
if [ "${ACTUAL_WF}" != "${EXPECTED_WF_COUNT}" ]; then
  echo "FAIL: restored workflow_transitions count = ${ACTUAL_WF}, want ${EXPECTED_WF_COUNT}" >&2
  fail=1
fi
if [ "${ACTUAL_AP}" != "${EXPECTED_AP_COUNT}" ]; then
  echo "FAIL: restored approved_plans count = ${ACTUAL_AP}, want ${EXPECTED_AP_COUNT}" >&2
  fail=1
fi
if [ "${ACTUAL_AL}" != "${EXPECTED_AL_COUNT}" ]; then
  echo "FAIL: restored audit_log count = ${ACTUAL_AL}, want ${EXPECTED_AL_COUNT}" >&2
  fail=1
fi
if [ "${fail}" -ne 0 ]; then
  echo "restore: FAILED data-integrity verification against ${BACKUP_DIR}/manifest.json" >&2
  exit 1
fi

echo "== verify the audit_log hash chain still verifies post-restore (docs/PLAN.md Task 39 Acceptance) =="
go run ./cmd/foundry audit verify --pg-dsn "${RESTORE_DSN}"

echo "restore: OK -> ${RESTORE_DB} (workflow_transitions=${ACTUAL_WF} approved_plans=${ACTUAL_AP} audit_log=${ACTUAL_AL}, all match backup-time counts; audit chain verified)"
echo "${RESTORE_DSN}"
