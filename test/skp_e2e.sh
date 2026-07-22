#!/usr/bin/env bash
# M0 exit proof (docs/PLAN.md Task 19 / SKP-17): `make skp-e2e` = doctor ->
# three plans (success, deterministic-fail, resume) -> `foundry evidence
# verify` on each -> status consistency check -> fitness -> archive
# evidence/histories -> docs/notes/m0-exit-report.md.
#
# Requires a live Postgres (migrations/0001-0003 applied) and a live
# Temporal reachable at TEMPORAL_HOSTPORT, same as test/skp_resume_test.sh
# and test/status_consistency_e2e.sh, which this script composes rather than
# reimplements. There is no Docker daemon in this task's execution
# environment (the same established blocker as Tasks 2/4/8/12-18), so this
# script has never been run live; it is provided as the real, complete
# validation path for an environment that has one (docs/PLAN.md §A "no
# self-reported done"). See docs/PLAN.md Task 19's Status line and
# docs/notes/m0-exit-report.md for exactly what was, and was not, executed.
#
# Composition, not reinvention:
#   - the "resume" plan step delegates to test/skp_resume_test.sh (a single
#     run here; the dedicated 20x loop remains `make skp-resume` per Task
#     16, and this report links to that CI job rather than re-running it
#     20x inside skp-e2e too).
#   - the "status consistency check" step delegates to
#     test/status_consistency_e2e.sh unchanged.
#   - the "success" and "deterministic-fail" plans are this script's own
#     minimal fixtures (goal repointed at test/fixtures/fake_scripts/
#     success.yaml and fail.yaml respectively), following the exact same
#     "adapt the plan shape for the fake executor" pattern
#     test/skp_resume_test.sh already established for two-task.md.
#
# Security: the only process this script ever signals is the exact PID it
# itself started via $! immediately after exec'ing the foundryd binary it
# built — never a pattern-matched pkill/pgrep.
set -euo pipefail

: "${PG_DSN:=postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable}"
: "${TEMPORAL_HOSTPORT:=temporal:7233}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

RUN_ID="$$"
RUN_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
WORKDIR="$(mktemp -d)"
FOUNDRYD_PID=""
cleanup() {
  if [ -n "${FOUNDRYD_PID}" ] && kill -0 "${FOUNDRYD_PID}" 2>/dev/null; then
    kill -9 "${FOUNDRYD_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

KEY_DIR="${WORKDIR}/keys"
WORKTREE_ROOT="${WORKDIR}/worktrees"
EVIDENCE_ROOT="${WORKDIR}/evidence"
REPO_PATH="${WORKDIR}/repo"
FOUNDRYD_BIN="${WORKDIR}/foundryd"
ARCHIVE_DIR="evidence/m0-exit/${RUN_ID}"

mkdir -p "${WORKTREE_ROOT}" "${EVIDENCE_ROOT}"

echo "== step 1/7: doctor =="
go run ./cmd/foundry doctor

echo "== apply migrations =="
psql "${PG_DSN}" -f internal/db/migrations/00001_approved_plans.sql
psql "${PG_DSN}" -f internal/db/migrations/00002_transitions.sql
psql "${PG_DSN}" -f internal/db/migrations/00003_projection.sql

echo "== build a real git repo for worktree.Manager to branch off =="
git init -b main "${REPO_PATH}" >/dev/null
git -C "${REPO_PATH}" -c user.name=e2e -c user.email=e2e@example.com commit --allow-empty -m init >/dev/null

go run ./cmd/foundry keygen --dir "${KEY_DIR}"

echo "== build foundryd once, so kill -9 (if ever needed) targets the exact binary this script started =="
go build -o "${FOUNDRYD_BIN}" ./cmd/foundryd

start_worker() {
  FOUNDRY_KEY_DIR="${KEY_DIR}" \
  FOUNDRY_WORKTREE_ROOT="${WORKTREE_ROOT}" \
  FOUNDRY_EVIDENCE_ROOT="${EVIDENCE_ROOT}" \
  PG_DSN="${PG_DSN}" \
  TEMPORAL_HOSTPORT="${TEMPORAL_HOSTPORT}" \
  "${FOUNDRYD_BIN}" &
  FOUNDRYD_PID=$!
  sleep 2
}

stop_worker() {
  if [ -n "${FOUNDRYD_PID}" ] && kill -0 "${FOUNDRYD_PID}" 2>/dev/null; then
    kill "${FOUNDRYD_PID}" 2>/dev/null || true
    wait "${FOUNDRYD_PID}" 2>/dev/null || true
  fi
  FOUNDRYD_PID=""
}

# run_plan builds a one-task plan pointing goal at a fake_scripts fixture,
# submits/approves/starts it, and polls for the given terminal status.
# Echoes "<workflow_id> <run_id>" on success.
run_plan() {
  local name="$1" fixture="$2" want_status="$3"
  local wf_id="skp-e2e-${name}-${RUN_ID}"
  local plan_id="plan-skp-e2e-${name}-${RUN_ID}"
  local plan_file="${WORKDIR}/${name}.md"

  psql "${PG_DSN}" -c "DELETE FROM workflow_transitions WHERE workflow_id = '${wf_id}';" >/dev/null
  psql "${PG_DSN}" -c "DELETE FROM workflow_status_projection WHERE workflow_id = '${wf_id}';" >/dev/null

  cat > "${plan_file}" <<EOF
---
id: ${plan_id}
title: SKP e2e ${name}
version: "1.0"
repos:
  - alias: primary
    url: https://github.com/example/skp-e2e-${name}
    branch: main
tasks:
  - id: t1
    goal: ${REPO_ROOT}/test/fixtures/fake_scripts/${fixture}
    commands:
      - noop
    validation_commands:
      - noop
    files: []
declared_effects:
  - kind: code
    target: noop
requested_permissions:
  - kind: repo-write
    target: primary
budget_usd: 1.0
---
## Rationale

skp-e2e ${name} fixture (docs/PLAN.md Task 19 / SKP-17): one task pointing
at test/fixtures/fake_scripts/${fixture}, run against the fake executor.
EOF

  go run ./cmd/foundry plan submit --submitter alice "${plan_file}" >&2
  go run ./cmd/foundry plan approve \
    --submitter alice \
    --key-dir "${KEY_DIR}" \
    --pg-dsn "${PG_DSN}" \
    "${plan_file}" >&2

  local start_out
  start_out=$(go run ./test/helpers/startplan \
    --temporal-hostport "${TEMPORAL_HOSTPORT}" \
    --workflow-id "${wf_id}" \
    --plan-id "${plan_id}" \
    --plan-file "${plan_file}" \
    --repo-path "${REPO_PATH}" \
    --executor fake)
  local run_id
  run_id=$(echo "${start_out}" | grep -o 'run_id=[^ ]*' | cut -d= -f2)

  local deadline=$((SECONDS + 60))
  local final_status=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    final_status=$(go run ./cmd/foundry status "${wf_id}" --fresh \
      --pg-dsn "${PG_DSN}" --temporal-hostport "${TEMPORAL_HOSTPORT}" \
      | grep '^status:' | awk '{print $2}') || true
    if [ "${final_status}" == "${want_status}" ]; then
      break
    fi
    sleep 2
  done
  if [ "${final_status}" != "${want_status}" ]; then
    echo "FAIL: workflow ${wf_id} ended in '${final_status}', want '${want_status}'" >&2
    exit 1
  fi

  echo "${wf_id} ${run_id}"
}

echo "== step 2/7: scenario success (deterministic pass via fake executor) =="
start_worker
SUCCESS_RESULT=$(run_plan success success.yaml SUCCEEDED)
SUCCESS_WF_ID=$(echo "${SUCCESS_RESULT}" | awk '{print $1}')
SUCCESS_RUN_ID=$(echo "${SUCCESS_RESULT}" | awk '{print $2}')
echo "success: workflow_id=${SUCCESS_WF_ID} run_id=${SUCCESS_RUN_ID}"

echo "== step 2/7: scenario deterministic-fail =="
FAIL_RESULT=$(run_plan fail fail.yaml FAILED)
FAIL_WF_ID=$(echo "${FAIL_RESULT}" | awk '{print $1}')
FAIL_RUN_ID=$(echo "${FAIL_RESULT}" | awk '{print $2}')
echo "fail: workflow_id=${FAIL_WF_ID} run_id=${FAIL_RUN_ID}"
stop_worker

echo "== step 2/7: scenario resume (single run; 20x loop is the dedicated 'make skp-resume' target) =="
bash test/skp_resume_test.sh

echo "== step 3/7: foundry evidence verify on the success/fail bundles =="
mapfile -t BUNDLE_IDS < <(find "${EVIDENCE_ROOT}" -mindepth 2 -name manifest.json -exec dirname {} \; | xargs -n1 basename)
if [ "${#BUNDLE_IDS[@]}" -eq 0 ]; then
  echo "FAIL: no evidence bundles found under ${EVIDENCE_ROOT}" >&2
  exit 1
fi
for id in "${BUNDLE_IDS[@]}"; do
  FOUNDRY_DATA_DIR="${WORKDIR}" go run ./cmd/foundry evidence verify "${id}"
done

echo "== step 4/7: status consistency check =="
bash test/status_consistency_e2e.sh

echo "== step 5/7: fitness =="
bash scripts/fitness.sh

echo "== step 6/7: archive evidence bundles + transition histories =="
mkdir -p "${ARCHIVE_DIR}/evidence" "${ARCHIVE_DIR}/history"
cp -r "${EVIDENCE_ROOT}/." "${ARCHIVE_DIR}/evidence/"
psql "${PG_DSN}" -c "\copy (SELECT * FROM workflow_transitions WHERE workflow_id IN ('${SUCCESS_WF_ID}','${FAIL_WF_ID}') ORDER BY workflow_id, seq) TO STDOUT WITH CSV HEADER" \
  > "${ARCHIVE_DIR}/history/transitions.csv"

echo "== step 7/7: append run to docs/notes/m0-exit-report.md =="
cat >> docs/notes/m0-exit-report.md <<EOF

## Run ${RUN_ID} (${RUN_DATE})

- success: workflow_id=${SUCCESS_WF_ID} run_id=${SUCCESS_RUN_ID}
- deterministic-fail: workflow_id=${FAIL_WF_ID} run_id=${FAIL_RUN_ID}
- resume: see \`make skp-resume\` CI job for the 20x proof
- evidence bundles archived: ${ARCHIVE_DIR}/evidence/
- transition history archived: ${ARCHIVE_DIR}/history/transitions.csv
EOF

echo "skp_e2e: OK — success, deterministic-fail, and resume plans all reached their expected terminal status; evidence verified; fitness green; archived to ${ARCHIVE_DIR}"
