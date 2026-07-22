#!/usr/bin/env bash
# docs/PLAN.md Task 18 (SKP-16), rule (e) — seeded-violation self-test. Proves
# each fitness check actually catches what it claims to catch: every fixture
# under test/fitness_seeds/ must make its corresponding check FAIL. A fitness
# suite that cannot be proven to catch violations isn't trustworthy.
set -euo pipefail

cd "$(dirname "$0")/.."

fitlint_bin="$(mktemp -d)/fitlint"
go build -o "${fitlint_bin}" ./cmd/fitlint

fail=0

expect_fail() {
  local name="$1"
  shift
  if "$@" >/tmp/fitness_selftest_out 2>&1; then
    echo "SELFTEST FAILED: ${name} was expected to fail but passed"
    cat /tmp/fitness_selftest_out
    fail=1
  else
    echo "ok: ${name} correctly failed"
  fi
}

expect_fail "enum lint on test/fitness_seeds/enum" \
  "${fitlint_bin}" enum test/fitness_seeds/enum

expect_fail "superseded-term lint on test/fitness_seeds/term" \
  "${fitlint_bin}" term test/fitness_seeds/term

expect_fail "stdlib-only import boundary on test/fitness_seeds/import_stdlib/state" \
  bash scripts/check_stdlib_only.sh ./test/fitness_seeds/import_stdlib/state

expect_fail "scm boundary on test/fitness_seeds/import_scm" \
  bash scripts/check_scm_boundary.sh test/fitness_seeds/import_scm

expect_fail "doc-link resolver on test/fitness_seeds/doclink" \
  "${fitlint_bin}" doclinks test/fitness_seeds/doclink

if [ "${fail}" -ne 0 ]; then
  echo "fitness-selftest FAILED: one or more seeds did not fail their check"
  exit 1
fi

echo "fitness-selftest OK: all seeds correctly fail"
