#!/usr/bin/env bash
# docs/PLAN.md Task 132 (PRF-01) — Personal venture LIVE proof harness.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO_ROOT}"

EVIDENCE_ROOT="${FOUNDRY_VENTURE_EVIDENCE:-evidence/m5-personal}"
mkdir -p "${EVIDENCE_ROOT}"

echo "==> Venture e2e: hermetic fixture tier"
go test ./internal/mission/... ./internal/spec/mockup/... ./internal/admission/... \
  ./internal/opportunity/... ./internal/observe/... -count=1 -race

if [[ "${RUN_VENTURE_LIVE:-}" != "1" ]]; then
  echo "OK: hermetic venture tier green (set RUN_VENTURE_LIVE=1 for live control-plane proof)"
  cat > "${EVIDENCE_ROOT}/README.md" <<EOF
# m5-personal evidence

Hermetic tier completed. Live control-plane proof requires RUN_VENTURE_LIVE=1
with real credentials (Postgres, Temporal, API-billed executor, Fly personal
app, Stripe test mode, dedicated Telegram test bot).
EOF
  exit 0
fi

echo "==> Venture LIVE proof (Task 132)"
for v in PG_DSN TEMPORAL_HOSTPORT; do
  if [[ -z "${!v:-}" ]]; then
    echo "FAIL: RUN_VENTURE_LIVE=1 requires ${v}" >&2
    exit 1
  fi
done
if ! go run ./cmd/foundry doctor >/dev/null 2>&1; then
  echo "FAIL: foundry doctor failed — Temporal/Postgres must be reachable" >&2
  exit 1
fi

export FOUNDRY_VENTURE_EVIDENCE="${EVIDENCE_ROOT}"
go test ./test/e2e/venture/... -count=1 -race -run Live

if [[ ! -f "${EVIDENCE_ROOT}/human-touches.json" ]]; then
  echo "FAIL: live proof must write ${EVIDENCE_ROOT}/human-touches.json" >&2
  exit 1
fi

python3 - <<'PY' "${EVIDENCE_ROOT}/human-touches.json"
import json, sys
with open(sys.argv[1]) as f:
    d = json.load(f)
if d.get("avoidable_count", 1) != 0:
    raise SystemExit(f"avoidable_count={d.get('avoidable_count')}")
print("avoidable_count=0")
PY

echo "OK: live venture proof archived under ${EVIDENCE_ROOT}"
