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

expect_fail "doc-link resolver (incl. dead anchor, Task 37 / FND-18) on test/fitness_seeds/doclink" \
  "${fitlint_bin}" doclinks test/fitness_seeds/doclink

expect_fail "authority boundary: scm/write imported outside kernel" \
  "${fitlint_bin}" authority ./test/fitness_seeds/authority/scmwrite_caller/...

expect_fail "authority boundary: pec-shaped package imports kernel" \
  "${fitlint_bin}" authority ./test/fitness_seeds/authority/pec_shaped/internal/pec/...

expect_fail "secrets leak scan on test/fitness_seeds/secretsleak" \
  "${fitlint_bin}" secretsleak test/fitness_seeds/secretsleak

# docs/PLAN.md Task 37 (FND-18) seeded violations.

expect_fail "duplicate Mermaid diagram D-ID on test/fitness_seeds/mermaidid" \
  "${fitlint_bin}" mermaidid test/fitness_seeds/mermaidid

expect_fail "single-source contract heuristic on test/fitness_seeds/contract" \
  "${fitlint_bin}" contract test/fitness_seeds/contract

expect_fail "container-inventory lint: untracked Dockerfile on test/fitness_seeds/containers" \
  "${fitlint_bin}" containers test/fitness_seeds/containers

# docs/PLAN.md Task 61 (TX-08) — TenX prohibition seed.
expect_fail "tenx prohibition: PR-creation symbol in tenx path" \
  bash scripts/check_tenx_prohibition.sh test/fitness_seeds/tenx_prohibition

# The composed-file-reproducibility check operates on the repo's real root
# AGENTS.md/CLAUDE.md (there is no path-scoped fixture for it), so this seed
# temporarily hand-edits AGENTS.md, proves the check fails, then restores it
# — via a trap so a failure mid-block can't leave the repo's real AGENTS.md
# corrupted. (scripts/doclint/ai-harness-repro.sh's own golden-rule step
# already recomposes AGENTS.md/CLAUDE.md as a side effect of running, which
# also restores them — this restore is a defensive belt-and-suspenders, not
# reliance on that side effect.)
if command -v ars >/dev/null 2>&1; then
  agents_backup="$(mktemp)"
  cp AGENTS.md "${agents_backup}"
  trap 'cp "${agents_backup}" AGENTS.md 2>/dev/null; rm -f "${agents_backup}"' EXIT
  echo "seeded hand-edit: not reproducible from .ai/ (docs/PLAN.md Task 37 / FND-18)" >>AGENTS.md
  expect_fail "composed-file reproducibility: hand-edited AGENTS.md drifts from ars compose" \
    bash scripts/doclint/ai-harness-repro.sh
  cp "${agents_backup}" AGENTS.md
  rm -f "${agents_backup}"
  trap - EXIT
else
  echo "SKIPPED: composed-file-reproducibility seed requires ars on PATH"
fi

if [ "${fail}" -ne 0 ]; then
  echo "fitness-selftest FAILED: one or more seeds did not fail their check"
  exit 1
fi

echo "fitness-selftest OK: all seeds correctly fail"
