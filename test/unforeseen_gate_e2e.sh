#!/usr/bin/env bash
set -euo pipefail

go test ./internal/mission/ -run UnforeseenGateRoundTrip -count=1
