#!/usr/bin/env bash
# Constitution Check (Task 1 v0): enum lint, superseded-term lint, import-boundary
# checks, PEC prohibitions, payload limits, and doc-link check are added by later
# tasks as those subsystems exist. This v0 only verifies the repo builds and every
# internal package carries a doc.go authority statement.
set -euo pipefail

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

echo "fitness OK"
