#!/usr/bin/env bash
# Constitution Check (docs/PLAN.md Task 18 / SKP-16 — the constitution's
# teeth: violations fail CI). Orchestrates:
#   (a) enum lint            — cmd/fitlint enum
#   (b) superseded-term lint — cmd/fitlint term
#   (c) import boundaries    — scripts/check_stdlib_only.sh, scripts/check_scm_boundary.sh
#   (d) doc-link resolver    — cmd/fitlint doclinks
# plus the Task 1 v0 checks (go vet, doc.go presence) this script already had.
#
# test/fitness_seeds/** is deliberately excluded from every check below — it
# holds fixtures that must FAIL; scripts/fitness_selftest.sh (make
# fitness-selftest) proves that. See cmd/fitlint's skipDirNames and the
# --exclude-dir=fitness_seeds in scripts/check_scm_boundary.sh.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "== fitness: go vet =="
go vet ./...

echo "== fitness: doc.go presence in internal/* =="
missing=0
for dir in internal/*/; do
  pkg="${dir%/}"
  if [ ! -f "${pkg}/doc.go" ]; then
    echo "MISSING doc.go: ${pkg}"
    missing=1
  fi
done
if [ "${missing}" -ne 0 ]; then
  echo "fitness FAILED: one or more internal packages are missing doc.go"
  exit 1
fi

echo "== fitness: building cmd/fitlint =="
fitlint_bin="$(mktemp -d)/fitlint"
go build -o "${fitlint_bin}" ./cmd/fitlint

echo "== fitness (a): enum lint =="
"${fitlint_bin}" enum internal cmd tools

echo "== fitness (b): superseded-term lint =="
"${fitlint_bin}" term .

echo "== fitness (c): import boundaries =="
bash scripts/check_stdlib_only.sh ./internal/state
bash scripts/check_scm_boundary.sh .

echo "== fitness (d): doc-link resolver =="
"${fitlint_bin}" doclinks . docs/foundry

echo "fitness OK"
