#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go run ./cmd/foundry product new --from-template -name demo-product -out "$tmp" >/dev/null
cd "$tmp/demo-product"
make test >/dev/null
bash e2e/smoke.sh >/dev/null
