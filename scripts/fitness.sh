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

echo "== fitness (i): TenX prohibition boundary (Task 61 / TX-08, Constitution C15) =="
bash scripts/check_tenx_prohibition.sh .

echo "== fitness (j): executor-capability staleness (Task 84 / PRV-01) =="
"${fitlint_bin}" capability config/executor-capabilities.yaml

echo "== fitness (k): opportunity-research boundary (Task 101 / OPP-02, Constitution C23) =="
"${fitlint_bin}" research-boundary internal

echo "== fitness (l): plan validation-command declaration (Task 104 / SKP-11R2, Constitution C10) =="
"${fitlint_bin}" plan-validation examples internal/admission/testdata

echo "== fitness (m): plan topology (Task 110 / INT-02) =="
"${fitlint_bin}" plan-topology docs/PLAN.md examples internal/admission/testdata

echo "== fitness (n): kernel bare-subprocess ban (Task 115 / SEC-01, Constitution C4/C24) =="
"${fitlint_bin}" subprocess ./internal/kernel

echo "== fitness (o): no process-global credential env on the executor/kernel path (Task 117 / SEC-03) =="
"${fitlint_bin}" env ./internal/executor ./internal/kernel

echo "fitness OK"
