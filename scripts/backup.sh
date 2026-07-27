#!/usr/bin/env bash
# `make backup` (docs/PLAN.md Task 39 / FND-20 M1 exit): pg_dump (custom
# format) of the Foundry Postgres database + a tar.gz of the evidence
# content-addressed store, into a timestamped, checksummed backup
# directory under backups/ (gitignored — these are runtime artifacts, not
# source).
#
# Custom format (`--format=custom`) is used rather than plain SQL because
# it is pg_restore's own recommended format for anything beyond a toy
# dump: compressed, supports parallel restore, and lets pg_restore
# recreate the target objects rather than requiring a pre-existing schema.
#
# A manifest.json records: pg_dump/evidence-tar sha256 (tamper detection
# at restore time — scripts/restore.sh refuses to restore a backup whose
# files don't match their own recorded checksum) and row counts for the
# tables this task's Acceptance cares about (workflow_transitions,
# approved_plans, audit_log) so restore-time data-integrity verification
# has a real "what should be there" baseline to compare against, not just
# "the restore command exited 0" (qa-testing / security-hardening A09).
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"
: "${FOUNDRY_EVIDENCE_ROOT:=./data/evidence}"
: "${BACKUP_ROOT:=backups}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${BACKUP_ROOT}/${TS}"
mkdir -p "${OUT_DIR}"

echo "== pg_dump --format=custom -> ${OUT_DIR}/foundry.dump =="
pg_dump --format=custom --file="${OUT_DIR}/foundry.dump" "${PG_DSN}"

echo "== tar evidence store -> ${OUT_DIR}/evidence.tar.gz =="
if [ -d "${FOUNDRY_EVIDENCE_ROOT}" ] && [ -n "$(find "${FOUNDRY_EVIDENCE_ROOT}" -mindepth 1 -print -quit 2>/dev/null)" ]; then
  tar -czf "${OUT_DIR}/evidence.tar.gz" -C "$(dirname "${FOUNDRY_EVIDENCE_ROOT}")" "$(basename "${FOUNDRY_EVIDENCE_ROOT}")"
else
  echo "no non-empty evidence store found at ${FOUNDRY_EVIDENCE_ROOT} -- writing an empty archive marker" >&2
  tar -czf "${OUT_DIR}/evidence.tar.gz" --files-from=/dev/null
fi

echo "== row counts at backup time (restore-time integrity baseline) =="
read -r WF_COUNT AP_COUNT AL_COUNT <<EOF
$(psql "${PG_DSN}" -tA -F' ' -c "
  SELECT
    (SELECT count(*) FROM workflow_transitions),
    (SELECT count(*) FROM approved_plans),
    (SELECT count(*) FROM audit_log)
")
EOF
echo "workflow_transitions=${WF_COUNT} approved_plans=${AP_COUNT} audit_log=${AL_COUNT}"

DUMP_SHA="$(sha256sum "${OUT_DIR}/foundry.dump" | awk '{print $1}')"
EVID_SHA="$(sha256sum "${OUT_DIR}/evidence.tar.gz" | awk '{print $1}')"

cat > "${OUT_DIR}/manifest.json" <<EOF
{
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "pg_dump": {
    "file": "foundry.dump",
    "format": "custom",
    "sha256": "${DUMP_SHA}"
  },
  "evidence": {
    "file": "evidence.tar.gz",
    "sha256": "${EVID_SHA}"
  },
  "row_counts": {
    "workflow_transitions": ${WF_COUNT},
    "approved_plans": ${AP_COUNT},
    "audit_log": ${AL_COUNT}
  }
}
EOF

echo "backup: OK -> ${OUT_DIR} (dump sha256=${DUMP_SHA:0:12}..., evidence sha256=${EVID_SHA:0:12}...)"
echo "${OUT_DIR}"
