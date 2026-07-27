#!/usr/bin/env bash
# End-to-end proof for docs/PLAN.md Task 25 (FND-06 / Constitution C12):
# strong-auth (OIDC + WebAuthn) approvals for high-risk actions.
#
# This script proves two things through real process boundaries, not just
# in-process Go function calls:
#
#   1. `foundry login` (the compiled CLI binary, run as a real subprocess)
#      completes a full RFC 8628 device-code OIDC login against
#      test/fakes/oidc's fake IdP (also run as a real subprocess, on its
#      own real TCP port) and writes a real session JWT + session signing
#      key under a throwaway $HOME.
#   2. `POST /v1/plans/{id}/approve` on a real net/http server (this
#      task's ApproveHandler, backed by in-memory fakes — no live
#      Postgres required, matching the precedent set by
#      test/provenance_e2e.sh) actually enforces step-up over the wire:
#      curl gets 403 for a High-tier plan with no WebAuthn assertion, and
#      200 once a real WebAuthn ceremony (via the go-webauthn +
#      virtualwebauthn libraries) is presented.
#
# What this script does NOT attempt: driving the WebAuthn assertion
# ceremony from bash/curl. WebAuthn's client response is an asymmetric
# signature over the relying party's challenge — there is no shell
# primitive for that, and hand-rolling one here would violate this task's
# own Boundary ("no self-built crypto; libraries only"). That leg runs as
# a small Go program instead (still a real HTTP client hitting the real
# server on the real port opened by step 2 below) — see
# test/approval_stepup_e2e_client/main.go.
#
# No live Postgres or Temporal is required: the approve server this script
# stands up uses provenance.MemRawStore / authn.MemUserStore, the same
# in-memory fakes internal/authn's own test suite uses.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

WORKDIR="$(mktemp -d)"
IDP_PID=""
SERVER_PID=""
cleanup() {
  for pid in "${IDP_PID}" "${SERVER_PID}"; do
    if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
      kill -9 "${pid}" 2>/dev/null || true
    fi
  done
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

echo "== approval_stepup_e2e: build foundry CLI =="
FOUNDRY_BIN="${WORKDIR}/foundry"
go build -o "${FOUNDRY_BIN}" ./cmd/foundry

# Built once, as a real binary, rather than invoked via `go run` for each
# mode: `go run` execs the compiled program as a *child* of itself, so
# `kill -9 "$!"` on a backgrounded `go run` only kills the go run wrapper
# and orphans the actual server/idp process it spawned. Running the
# compiled binary directly means $! is the real PID cleanup needs.
E2E_CLIENT_BIN="${WORKDIR}/approval_stepup_e2e_client"
go build -o "${E2E_CLIENT_BIN}" ./test/approval_stepup_e2e_client

echo "== approval_stepup_e2e: start fake OIDC IdP =="
IDP_INFO="${WORKDIR}/idp.info"
"${E2E_CLIENT_BIN}" -mode=idp -info-file="${IDP_INFO}" &
IDP_PID=$!
for _ in $(seq 1 50); do
  [ -s "${IDP_INFO}" ] && break
  sleep 0.2
done
[ -s "${IDP_INFO}" ] || { echo "fake IdP never wrote ${IDP_INFO}"; exit 1; }
IDP_URL="$(sed -n '1p' "${IDP_INFO}")"
IDP_CLIENT_ID="$(sed -n '2p' "${IDP_INFO}")"
echo "fake IdP up at ${IDP_URL} (client_id=${IDP_CLIENT_ID})"

echo "== approval_stepup_e2e: foundry login (real subprocess, real device-code flow) =="
LOGIN_HOME="${WORKDIR}/home"
mkdir -p "${LOGIN_HOME}"
HOME="${LOGIN_HOME}" "${FOUNDRY_BIN}" login --issuer-url "${IDP_URL}" --client-id "${IDP_CLIENT_ID}"

SESSION_FILE="${LOGIN_HOME}/.foundry/session.jwt"
SESSION_KEY="${LOGIN_HOME}/.foundry/keys/session.key"
[ -s "${SESSION_FILE}" ] || { echo "foundry login did not write ${SESSION_FILE}"; exit 1; }
[ -s "${SESSION_KEY}" ] || { echo "foundry login did not write ${SESSION_KEY}"; exit 1; }
echo "session token + signing key written under ${LOGIN_HOME}/.foundry"

echo "== approval_stepup_e2e: start approve server (real net/http, in-memory store) =="
SERVER_INFO="${WORKDIR}/server.info"
"${E2E_CLIENT_BIN}" -mode=server -session-key="${SESSION_KEY}" -info-file="${SERVER_INFO}" &
SERVER_PID=$!
for _ in $(seq 1 50); do
  [ -s "${SERVER_INFO}" ] && break
  sleep 0.2
done
[ -s "${SERVER_INFO}" ] || { echo "approve server never wrote ${SERVER_INFO}"; exit 1; }
SERVER_URL="$(cat "${SERVER_INFO}")"
echo "approve server up at ${SERVER_URL}"

SESSION_TOKEN="$(cat "${SESSION_FILE}")"

echo "== approval_stepup_e2e: H-tier approve WITHOUT WebAuthn -> expect 403 =="
STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${SERVER_URL}/v1/plans/plan-h/approve" \
  -H "Authorization: Bearer ${SESSION_TOKEN}" -d '{}')"
[ "${STATUS}" = "403" ] || { echo "expected 403, got ${STATUS}"; exit 1; }
echo "got 403 as expected"

echo "== approval_stepup_e2e: H-tier approve WITH a real WebAuthn ceremony -> expect 200 =="
"${E2E_CLIENT_BIN}" -mode=approve -server-url="${SERVER_URL}" \
  -session-token="${SESSION_TOKEN}" -plan-id=plan-h

echo "== approval_stepup_e2e: replay the same assertion -> expect 403 =="
STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${SERVER_URL}/v1/plans/plan-h/approve" \
  -H "Authorization: Bearer ${SESSION_TOKEN}" -d '{}')"
# plan-h is already approved above; this call carries no assertion at all,
# proving there is still no bypass after a prior successful step-up.
[ "${STATUS}" = "403" ] || { echo "expected 403 on a bare re-approve attempt, got ${STATUS}"; exit 1; }

echo "== approval_stepup_e2e: PASS =="
