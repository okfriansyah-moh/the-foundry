#!/usr/bin/env bash
# docs/PLAN.md Task 61 (TX-08) + Task 108 (RTC-04): TenX prohibition check
# (Constitution C15). Asserts that no PR-opening, merge, staging-deploy or
# production-deploy surface is reachable from the TenXDeliver workflow — checked
# against the workflow and every file in its 10x call path, not a single file.
set -euo pipefail

root="${1:-.}"

files=(
  "${root%/}/internal/kernel/tenx_workflow.go"
  "${root%/}/internal/kernel/tenx_integrate.go"
  "${root%/}/internal/kernel/tenx_policy.go"
)
for f in "${root%/}"/internal/kernel/integrator/*.go; do
  case "${f}" in
    *_test.go) continue ;;
  esac
  files+=("${f}")
done

found=0
present=0
# POSIX ERE (grep -E) has no \b word-boundary metacharacter, so use explicit
# non-identifier boundaries instead — \b would be a literal backspace/no-op and
# could let a prohibited symbol slip the check (Constitution C15 bypass).
pattern='(^|[^A-Za-z0-9_])(CreatePullRequest|OpenPullRequest|MergePullRequest|CreateMergeRequest|MergeBranch|DeployTo|StagingDeploy|ProductionDeploy)([^A-Za-z0-9_]|$)'
for file in "${files[@]}"; do
  [ -f "${file}" ] || continue
  present=1
  matches="$(grep -nE "${pattern}" "${file}" 2>/dev/null || true)"
  if [ -n "${matches}" ]; then
    echo "VIOLATION: prohibited PR/merge/staging/deploy surface reachable from TenXDeliver in ${file}:"
    echo "${matches}"
    found=1
  fi
done

wf="${root%/}/internal/kernel/tenx_integrate.go"
if [ -f "${wf}" ]; then
  bad_activity="$(grep -nE 'ExecuteActivity\([^,]+, *"[^"]*(PullRequest|Merge|Deploy|Staging|Production)[^"]*"' "${wf}" 2>/dev/null || true)"
  if [ -n "${bad_activity}" ]; then
    echo "VIOLATION: TenXDeliver dispatches a prohibited activity:"
    echo "${bad_activity}"
    found=1
  fi
fi

if [ "${present}" -eq 0 ]; then
  echo "tenx prohibition: no 10x call-path files found under ${root}" >&2
  exit 1
fi
if [ "${found}" -ne 0 ]; then
  exit 1
fi

echo "tenx prohibition OK: TenXDeliver call path clean"
