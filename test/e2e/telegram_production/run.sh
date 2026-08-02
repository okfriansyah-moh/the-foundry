#!/usr/bin/env bash
set -euo pipefail
go test ./internal/notify/... ./test/e2e/telegram_production/... -count=1
echo "==> telegram_production: PASS (unit); live Bot API gated on credentials"
