#!/usr/bin/env bash
set -euo pipefail
# Invoked by make v1-proof after credential gate.
go test ./test/e2e/v1/... -count=1
