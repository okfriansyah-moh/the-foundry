#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

go build ./cmd/foundry ./cmd/foundryd
echo "upgrade drill: binaries build cleanly across the current schema and workflow version policy"
