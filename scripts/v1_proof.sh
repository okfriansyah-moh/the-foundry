#!/usr/bin/env bash
# docs/PLAN.md Task 151: protected V1 release proof entrypoint.
# Absence of required live infrastructure is FAILURE, never skip-success.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

required_vars=(
  PG_DSN
  TEMPORAL_HOSTPORT
  FOUNDRY_EVIDENCE_STORE
  TELEGRAM_BOT_TOKEN
  OIDC_ISSUER
  FOUNDRY_LLM_PROVIDER
  FLY_API_TOKEN
  STRIPE_TEST_KEY
  BITBUCKET_TOKEN
)

missing=()
for v in "${required_vars[@]}"; do
  if [[ -z "${!v:-}" ]]; then
    missing+=("$v")
  fi
done

if ((${#missing[@]})); then
  echo "v1-proof FAILED: missing required credentials/infrastructure:"
  printf '  - %s\n' "${missing[@]}"
  echo "Local diagnostic-only skip requires V1_PROOF_ALLOW_SKIP=1 (does NOT write PASS evidence)."
  if [[ "${V1_PROOF_ALLOW_SKIP:-}" == "1" ]]; then
    echo "v1-proof SKIPPED (explicit local diagnostic; not a release PASS)"
    exit 2
  fi
  exit 1
fi

echo "==> v1-proof: environment identities present; running Proofs A–F harness"
go test ./test/e2e/v1/... -count=1 -timeout 60m
echo "==> v1-proof: PASS"
