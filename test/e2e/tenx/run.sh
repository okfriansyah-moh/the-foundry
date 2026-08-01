#!/usr/bin/env bash
# docs/PLAN.md Task 133 (PRF-02) — 10x LIVE proof against a disposable Bitbucket remote.
#
# Fail-closed: when Temporal/Postgres are absent this harness FAILS (never
# exit 0). Live remote push requires RUN_TENX_LIVE=1 and disposable-repo markers.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO_ROOT}"

EVIDENCE_ROOT="${FOUNDRY_TENX_EVIDENCE:-evidence/m5-tenx}"
mkdir -p "${EVIDENCE_ROOT}"

echo "==> TenX prohibition static proof"
bash scripts/check_tenx_prohibition.sh .

echo "==> TenX unit / workflow contracts"
go test ./test/e2e/tenx/... ./internal/kernel/... ./internal/scm/... -count=1 -race -run 'TenX|Contract|SelectSCM|Bitbucket'

if ! go run ./cmd/foundry doctor >/dev/null 2>&1; then
  echo "FAIL: Temporal/PostgreSQL not available — Task 133 refuses false-green skip" >&2
  exit 1
fi

if [[ "${RUN_TENX_LIVE:-}" != "1" ]]; then
  echo "OK: infra present; hermetic TenX contracts green (set RUN_TENX_LIVE=1 for disposable remote push)"
  cat > "${EVIDENCE_ROOT}/README.md" <<EOF
# m5-tenx evidence

Hermetic contracts green with live Temporal/Postgres. Disposable Bitbucket
remote proof requires RUN_TENX_LIVE=1 plus:
  BITBUCKET_API_TOKEN
  SCM_WRITE_TEST_BITBUCKET_REPO_URL (must be marked disposable)
  SCM_WRITE_TEST_BITBUCKET_BASE_BRANCH
  FOUNDRY_TENX_DISPOSABLE=1
EOF
  exit 0
fi

: "${BITBUCKET_API_TOKEN:?RUN_TENX_LIVE=1 requires BITBUCKET_API_TOKEN}"
: "${SCM_WRITE_TEST_BITBUCKET_REPO_URL:?RUN_TENX_LIVE=1 requires disposable repo URL}"
: "${SCM_WRITE_TEST_BITBUCKET_BASE_BRANCH:?RUN_TENX_LIVE=1 requires base branch}"
if [[ "${FOUNDRY_TENX_DISPOSABLE:-}" != "1" ]]; then
  echo "FAIL: refusing to push — set FOUNDRY_TENX_DISPOSABLE=1 to confirm disposable remote" >&2
  exit 1
fi

echo "==> TenX LIVE disposable remote proof"
export FOUNDRY_TENX_EVIDENCE="${EVIDENCE_ROOT}"
go test ./test/e2e/tenx/... -count=1 -race -run Live

echo "OK: live TenX proof archived under ${EVIDENCE_ROOT}"
