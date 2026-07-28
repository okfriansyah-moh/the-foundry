#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go run ./cmd/foundry product new --from-template -name synthetic-check -out "$tmp" >/dev/null
bash "$tmp/synthetic-check/e2e/smoke.sh" >/dev/null
go test ./internal/verify/synthetic/... >/dev/null
