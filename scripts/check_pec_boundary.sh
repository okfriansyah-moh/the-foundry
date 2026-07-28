#!/usr/bin/env bash
# docs/PLAN.md Task 56 (TX-03): PEC import-boundary check (Constitution C5).
#
# internal/pec may import only: plan, state, verify, executor (Summary type).
# Importing kernel, scm, ledger, provenance, database drivers, or net/http
# from internal/pec is a fitness violation.
#
# Usage: check_pec_boundary.sh <root>...
set -euo pipefail

module="$(go list -m)"
forbidden=(
  "${module}/internal/kernel"
  "${module}/internal/scm"
  "${module}/internal/ledger"
  "${module}/internal/provenance"
  "database/sql"
  "github.com/jackc/pgx"
  "go.temporal.io/sdk/client"
  "net/http"
)

found=0
for pkg in "${forbidden[@]}"; do
  for dir in "$@"; do
    matches="$(grep -rn --include='*.go' --exclude-dir=fitness_seeds -F "\"${pkg}" "${dir}/internal/pec" 2>/dev/null || true)"
    if [ -n "${matches}" ]; then
      echo "VIOLATION: internal/pec imports forbidden package ${pkg}:"
      echo "${matches}"
      found=1
    fi
  done
done

if [ "${found}" -ne 0 ]; then
  exit 1
fi
exit 0
