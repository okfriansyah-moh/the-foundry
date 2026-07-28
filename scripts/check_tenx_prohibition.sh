#!/usr/bin/env bash
# docs/PLAN.md Task 61 (TX-08): TenX prohibition check (Constitution C15).
# Rejects PR/merge/staging/deploy call-surface symbols in the TenX call path.
set -euo pipefail

root="${1:-.}"
file="${root%/}/internal/kernel/tenx_workflow.go"
if [ ! -f "${file}" ]; then
  file="${root%/}/violation.txt"
fi
if [ ! -f "${file}" ]; then
  echo "tenx prohibition: no tenx_workflow.go or violation.txt under ${root}" >&2
  exit 1
fi

pattern='\b(CreatePullRequest|OpenPullRequest|MergePullRequest|CreateMergeRequest|MergeBranch|DeployTo|StagingDeploy|ProductionDeploy)\b'
matches="$(grep -nE "${pattern}" "${file}" 2>/dev/null || true)"
if [ -n "${matches}" ]; then
  echo "VIOLATION: prohibited PR/merge/staging/deploy surface found in ${file}:"
  echo "${matches}"
  exit 1
fi

echo "tenx prohibition OK: ${file}"
