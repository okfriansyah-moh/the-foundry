#!/usr/bin/env bash
# docs/PLAN.md Task 18 (SKP-16), rule (c) — half of the import-boundary check:
# the given package must depend on the standard library only. Stdlib import
# paths never contain a dot in their first path segment; anything else
# (github.com/..., golang.org/x/..., go.temporal.io/...) does. The package
# under test is excluded from that check since it always "depends on itself".
#
# Usage: check_stdlib_only.sh <go-package-pattern>
set -euo pipefail

pkg="${1:?usage: check_stdlib_only.sh <go-package-pattern>}"

deps="$(go list -deps "${pkg}")"
self="$(go list "${pkg}")"

violations=0
while IFS= read -r dep; do
  [ "${dep}" = "${self}" ] && continue
  first_segment="${dep%%/*}"
  if [[ "${first_segment}" == *.* ]]; then
    echo "VIOLATION: ${pkg} depends on non-stdlib package: ${dep}"
    violations=1
  fi
done <<<"${deps}"

exit "${violations}"
