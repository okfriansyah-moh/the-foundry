#!/usr/bin/env bash
# Task 113 (INT-05) — Telegram idea intake → mission draft, confirmation-required.
#
# Proves, with zero network, that a free-text /idea message from a bound
# principal produces a draft and spends nothing until an explicit /confirm; that
# an unbound/unpermitted chat is refused with no state change; that a message
# claiming its own authorization changes nothing; that a budget above the
# principal's cap is clamped and the clamp is what gets confirmed; that a
# replayed /confirm is rejected; and that an H-tier draft is refused with a
# pointer to strong auth.
set -euo pipefail

echo "==> [1] /idea → draft → /confirm contract (clamp, replay, self-auth, H-tier, parse-fail)"
go test ./internal/notify/... -run "Idea" -race -count=1 -v 2>&1 \
  | grep -E "^--- (PASS|FAIL)|^(ok|FAIL)"

echo "==> [2] Red-team: prompt-injection, budget inflation, stale /confirm replay"
go test -tags redteam ./test/redteam/... -run "TelegramInjection" -count=1 -v 2>&1 \
  | grep -E "^--- (PASS|FAIL)|^(ok|FAIL)"

echo "telegram_idea_e2e: all checks passed"
