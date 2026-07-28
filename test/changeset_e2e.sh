#!/usr/bin/env bash
# docs/PLAN.md Task 78 (EVO-05) — multi-repository 10x change-set saga e2e.
#
# Proves against the real internal/kernel change-set resolver, over 3 fixture
# repos including one seeded failure: ordered integration, parallel-child
# isolation, and exact all-or-honest-partial receipt-map semantics with no
# auto-revert of shared branches.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== changeset e2e: driving real internal/kernel change-set saga =="
go run ./test/helpers/changeset/

echo "== changeset e2e: unit saga semantics =="
go test ./internal/kernel/ -run 'ChangeSet|FreezeContracts' -race

echo "changeset e2e OK"
