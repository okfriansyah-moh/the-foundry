#!/usr/bin/env bash
# Task 52 (VEN-13) + Task 112 (INT-04) — veto digest e2e, real round-trip.
#
# Task 112 replaces this script's former single `go test` line with a real
# digest round-trip through the wired engine: the digest is coalesced, enqueued,
# delivered through the fake Telegram server (sendMessage), and the veto/rollback
# semantics are asserted end to end — plus expired-veto-ignored and
# loop-continues-during-window.
set -euo pipefail

echo "==> [1] Digest coalescing + veto/rollback + freeze semantics"
go test ./internal/notify/... -run "Digest|Veto|Freeze" -race -count=1 -v 2>&1 \
  | grep -E "^--- (PASS|FAIL)|^(ok|FAIL)"

echo "==> [2] Engine delivery round-trip through the fake Telegram server"
go test ./internal/notify/... -run "Engine|Deliver|Batch" -race -count=1 2>&1 \
  | grep -E "^(ok|FAIL)"

echo "veto_digest_e2e: all checks passed"
