#!/usr/bin/env bash
# Task 141 (RTC-05) — immutable execution envelope live-path checks.
set -euo pipefail

echo "==> [1] Kernel envelope unit + replay + start binding"
go test ./internal/kernel/ -race -count=1 -run 'TestExecutionEnvelope|TestStartDelivery_Resolves|TestStartDelivery_Unattended' 2>&1 | grep -E "ok|FAIL|PASS"

echo "==> [2] Envelope e2e package"
go test ./test/e2e/envelope/... -race -count=1 -v 2>&1 | grep -E "RUN|PASS|FAIL|ok "

echo "==> envelope e2e: PASS"
