#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../../.."

bash scripts/check_tenx_prohibition.sh .
go test ./test/e2e/tenx/... ./internal/kernel -run TenX -count=1

if ! go run ./cmd/foundry doctor >/dev/null 2>&1; then
  echo "SKIP: Temporal/PostgreSQL not available; prohibition proof completed."
  exit 0
fi

echo "Temporal detected; running TenX adapter contracts."
go test ./internal/scm/... -run Contract -count=1
