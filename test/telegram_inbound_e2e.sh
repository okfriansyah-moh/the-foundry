#!/usr/bin/env bash
# Task 112 (INT-04) — Telegram inbound transport e2e.
#
# Drives the real getUpdates receiver against test/fakes/telegram (extended with
# getUpdates), asserting a command arrives, routes and is answered, and that the
# durable offset makes a restart neither lose nor replay an update — with no
# live Telegram credentials and no network.
#
# The durable-pacing and offset persistence against Postgres are exercised by
# the notify store tests under a live-DB gate; this script covers the
# transport/routing/restart contract without infra.

set -euo pipefail

echo "==> [1] Inbound receiver: routing, durable offset, restart-no-replay, ingress guard"
go test ./internal/notify/... -run "Receiver" -race -count=1 -v 2>&1 \
  | grep -E "^--- (PASS|FAIL)|^(ok|FAIL)"

echo "==> [2] Fake Telegram server getUpdates + sendMessage round-trip"
go test ./internal/notify/... -run "Receiver_RoutesCommandAndAdvancesOffset" -race -count=1 2>&1 \
  | grep -E "^(ok|FAIL)"

echo "telegram_inbound_e2e: all checks passed"
