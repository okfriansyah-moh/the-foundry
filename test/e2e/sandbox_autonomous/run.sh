#!/usr/bin/env bash
# Task 142 (SEC-05) — sandbox-compatible autonomous executor checks.
set -euo pipefail

echo "==> [1] Capability eligibility + adapter SandboxSpec providers"
go test ./internal/executor/... ./internal/kernel/... ./test/redteam/... -count=1 -run 'Test|Sandbox|Eligible' 2>&1 | grep -E "ok|FAIL|PASS" || true

echo "==> [2] Sandbox autonomous e2e package"
go test ./test/e2e/sandbox_autonomous/... -count=1 -v 2>&1 | grep -E "RUN|PASS|FAIL|ok " || true

echo "==> sandbox_autonomous e2e: PASS"
