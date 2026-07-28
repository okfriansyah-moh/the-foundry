#!/usr/bin/env bash
# Task 55 (TX-02) — org provenance e2e
# Three scenarios: tampered source digest, missing QA approver, valid path.
set -euo pipefail

go test ./internal/provenance/... -run "Org" -race -count=1 -v 2>&1 | grep -E "^--- (PASS|FAIL)|PASS|FAIL"

echo "org_provenance_e2e: all checks passed"
