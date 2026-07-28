#!/usr/bin/env bash
# Task 52 (VEN-13) — veto digest e2e
# Verifies: digest→veto→rollback semantics, expired veto ignored, loop continues during window.
set -euo pipefail

go test ./internal/notify/... -run "Digest|Veto|Freeze" -race -count=1 -v 2>&1 | grep -E "^--- (PASS|FAIL)|PASS|FAIL"

echo "veto_digest_e2e: all checks passed"
