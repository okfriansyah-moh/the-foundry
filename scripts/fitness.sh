#!/usr/bin/env bash
# Constitution Check (docs/PLAN.md Task 18 / SKP-16 — the constitution's
# teeth: violations fail CI). Orchestrates:
#   (a) enum lint            — cmd/fitlint enum
#   (b) superseded-term lint — cmd/fitlint term
#   (c) import boundaries    — scripts/check_stdlib_only.sh, scripts/check_scm_boundary.sh
#   (d) documentation lints  — scripts/doclint/run.sh: doc-link resolver +
#       anchor checking, duplicate Mermaid D-ID detector, single-source
#       contract heuristic, container-inventory lint, composed-file
#       reproducibility (docs/PLAN.md Task 37 / FND-18 — also `make doclint`
#       standalone; absorbs and retires Task 2's scripts/check-ai-harness.sh)
#   (e) authority boundary   — cmd/fitlint authority (docs/PLAN.md Task 28 / FND-09)
#   (f) secrets leak scan     — cmd/fitlint secretsleak (docs/PLAN.md Task 35 / FND-16)
#   (g) mission loop contract — cmd/fitlint missionloop (docs/PLAN.md Task 40 / VEN-01):
#       MissionLoop must refuse to start without a registered loop contract
#       (mission-contract.md §3) — structurally proven, not just documented.
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

echo "== fitness (d): documentation lints (docs/PLAN.md Task 37 / FND-18) =="
bash scripts/doclint/run.sh

echo "== fitness (e): authority import boundary (Task 28 / FND-09) =="
"${fitlint_bin}" authority ./internal/... ./cmd/... ./tools/...

echo "== fitness (f): secrets leak scan (Task 35 / FND-16) =="
"${fitlint_bin}" secretsleak .

echo "== fitness (g): mission loop contract (Task 40 / VEN-01) =="
"${fitlint_bin}" missionloop internal cmd tools

echo "== fitness (h): PEC import boundary (Task 56 / TX-03, Constitution C5) =="
bash scripts/check_pec_boundary.sh .

echo "fitness OK"
