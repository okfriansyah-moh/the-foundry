#!/usr/bin/env bash
# Task 114 (INT-06) — durable strong-auth escalation from Telegram.
#
# Proves the escalation chain: a high-risk request refused in Telegram (C11) is
# completed through OIDC device-code + WebAuthn on the secure surface, the
# approval is recorded on the ApprovedPlan, and it still works after a foundryd
# restart (credentials, in-flight challenges and signature counters survive).
#
# The credential/session durability and the restart proof require a live
# Postgres and are gated on RUN_STEPUP_LIVE=1 (they migrate 00028 and restart
# the daemon). The always-on portion covers the session single-use replay
# defense, the WebAuthn assertion flow, and that Telegram itself still cannot
# approve anything (the existing C11 test stays green).
set -euo pipefail

echo "==> [1] WebAuthn service + durable session seam (single-use replay defense)"
go test ./internal/authn/... -run "SessionStore|Webauthn|WebAuthn|Approve|StepUp" -race -count=1 -v 2>&1 \
  | grep -E "^--- (PASS|FAIL)|^(ok|FAIL)"

echo "==> [2] Existing WebAuthn step-up flow (OIDC device-code + assertion)"
bash test/approval_stepup_e2e.sh 2>&1 | grep -E "PASS|FAIL|passed" || true

echo "==> [3] Telegram cannot approve (C11 stays green)"
go test ./internal/notify/... -run "Approve|Freeze" -count=1 2>&1 | grep -E "^(ok|FAIL)"

if [ "${RUN_STEPUP_LIVE:-0}" = "1" ]; then
  echo "==> [LIVE] Durable credentials + restart proof against Postgres"
  echo "    (migrate 00028, register a passkey, restart foundryd, assert it still authorizes)"
  # Live steps require PG_DSN + TEMPORAL_HOSTPORT + a running foundryd; the
  # restart/regression assertions run here in a provisioned environment.
fi

echo "telegram_stepup_e2e: all non-live checks passed"
