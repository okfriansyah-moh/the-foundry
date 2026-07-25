#!/usr/bin/env bash
# docs/PLAN.md Task 18 (SKP-16), rule (c) — other half of the import-boundary
# check: internal/scm push symbols may only be referenced from internal/kernel
# (Constitution C4 — only the kernel performs side effects). internal/scm has
# no exported symbols yet beyond its Task-1 placeholder doc.go, so this check
# is a no-op today; it activates fully once Task 28 gives internal/scm/write
# real symbols and internal/kernel a real caller, per the task card's own
# note ("activated fully at Task 28"). It is wired in now so the boundary is
# enforced from the moment there is anything to enforce.
#
# Usage: check_scm_boundary.sh <root>...
set -euo pipefail

module="$(go list -m)"
scm_import="${module}/internal/scm"

matches="$(grep -rn --include='*.go' --exclude-dir=fitness_seeds -F "\"${scm_import}\"" "$@" \
  | grep -v -E '(^|/)internal/kernel/' \
  | grep -v -E '(^|/)internal/scm/' \
  || true)"

if [ -n "${matches}" ]; then
  echo "VIOLATION: internal/scm referenced outside internal/kernel:"
  echo "${matches}"
  exit 1
fi
exit 0
