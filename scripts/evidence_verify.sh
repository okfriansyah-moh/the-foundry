#!/usr/bin/env bash
# Verify evidence markers / content-addressed bundles.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
DATA_DIR="${FOUNDRY_DATA_DIR:-data}"
EVIDENCE_ROOT="${DATA_DIR}/evidence"
failed=0

if [[ -d "$EVIDENCE_ROOT" ]]; then
  while IFS= read -r m; do
    id="$(basename "$(dirname "$m")")"
    if ! go run ./cmd/foundry evidence verify "$id"; then
      failed=1
    fi
  done < <(find "$EVIDENCE_ROOT" -name manifest.json 2>/dev/null || true)
fi

# Task evidence directories required for the closed M6/M7 tasks.
for d in evidence/task-14{1,2,3,4,5,6,7,8,9} evidence/task-15{0,1,2,3,4,5} evidence/v1-final-gate; do
  if [[ -d "$d" ]]; then
    if [[ ! -f "$d/README.md" && ! -f "$d/index.json" ]]; then
      echo "MISSING evidence marker in $d"
      failed=1
    fi
  fi
done

if ((failed)); then
  echo "evidence-verify: FAIL"
  exit 1
fi
echo "evidence-verify: PASS"
