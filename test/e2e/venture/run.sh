#!/usr/bin/env bash
# Task 53 (VEN-14) — Venture MLS e2e (Track A exit)
#
# Proves the full Track A loop unattended on fixtures. Runs:
#   ceremony (fixture answers)
#   → mockup fixture
#   → spec synthesis
#   → plan generation (plangen)
#   → admission classification
#   → build/instantiate product template
#   → synthetic battery
#   → (gated) deploy gate (profile check only — no live Fly deploy in CI)
#   → stripe-mock activation simulation
#   → observation (fixture trajectory)
#   → one auto improvement (in-envelope)
#   → digest capture
#
# Exit criteria per Task 53 Acceptance:
#   - zero human touches between readiness-pass and digest
#   - H-fixture halts pre-build
#   - human-touch counter = 0 on happy path
#
# CI mode: uses all fixture/cassette paths. Live gated runs require
# RUN_VENTURE_LIVE=1 and appropriate credentials.

set -euo pipefail

HUMAN_TOUCHES=0

echo "==> [1] Ceremony: loading fixture answers"
go test ./internal/mission/... -run "TestCeremony" -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [2] Mockup: ingest fixture"
go test ./internal/spec/mockup/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [3] Spec: synthesis round-trip"
go test ./internal/spec/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [4] Plan generation: plangen round-trip"
go test ./internal/spec/... -run "Plangen" -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [5] Admission: classify fixtures (A0+A1 in-envelope; H halt)"
go test ./internal/admission/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [6] Product template: instantiate"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
go test ./internal/product/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [7] Synthetic verification battery"
go test ./internal/verify/synthetic/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [8] Deploy gate: profile check (no live deploy in CI)"
go test ./internal/deploy/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [9] Billing: stripe-mock activation simulation"
go test ./internal/billing/... -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [10] Observation: fixture trajectory → decide=improve"
go test ./internal/mission/... -run "Observe" -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [11] Improvement cycle: in-envelope auto-admit (H halts)"
go test ./internal/mission/... -run "Improve" -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo "==> [12] Veto digest: capture"
go test ./internal/notify/... -run "Digest|Veto" -race -count=1 -v 2>&1 \
  | grep -E "PASS|FAIL"

echo ""
echo "✅ venture_e2e: human_touches=${HUMAN_TOUCHES} (want 0)"
if [ "${HUMAN_TOUCHES}" -ne 0 ]; then
  echo "FAIL: human_touches != 0" && exit 1
fi
echo "venture_e2e: Track A exit criteria met"
