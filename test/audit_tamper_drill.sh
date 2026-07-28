#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./internal/audit/... -count=1
if [ -z "${PG_DSN:-}" ]; then
  echo "SKIP: live tamper drill requires PG_DSN; unit tamper proof completed."
  exit 0
fi
echo "PG_DSN set: run 'foundry audit verify --pg-dsn "$PG_DSN"' after staging a scratch tamper."
