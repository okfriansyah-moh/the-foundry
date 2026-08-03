#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO_ROOT}"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

BIN="${TMP_ROOT}/foundry"
PRODUCT_PARENT="${TMP_ROOT}/products"
WORKSPACE="${PRODUCT_PARENT}/packaging-proof"
EVIDENCE_ROOT="${FOUNDRY_PACKAGING_EVIDENCE:-${TMP_ROOT}/evidence}"
mkdir -p "${PRODUCT_PARENT}" "${EVIDENCE_ROOT}"

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

snapshot_tree() {
  local root="$1"
  local output="$2"
  (
    cd "${root}"
    find . -type f -print | LC_ALL=C sort | sed 's#^\./##'
  ) > "${output}"
}

snapshot_digests() {
  local root="$1"
  local output="$2"
  (
    cd "${root}"
    find . -type f -print | LC_ALL=C sort | while IFS= read -r file; do
      printf '%s  %s\n' "$(hash_file "${file}")" "${file#./}"
    done
  ) > "${output}"
}

snapshot_authority() {
  local output="$1"
  local paths=(
    config/executor-routing.yaml
    config/policy/platform.yaml
    config/profiles/organization-10x.yaml
    config/profiles/personal-autonomous-venture.yaml
    config/sandbox-egress-allowlist.yaml
    config/validation-allowlist.yaml
    deploy
    internal/scm
  )
  find "${paths[@]}" -type f -print | LC_ALL=C sort | while IFS= read -r file; do
    printf '%s  %s\n' "$(hash_file "${file}")" "${file}"
  done > "${output}"
}

echo "==> build and instantiate product"
go build -o "${BIN}" ./cmd/foundry
"${BIN}" product new --from-template -name packaging-proof -out "${PRODUCT_PARENT}" >/dev/null

# The template enables every catalog package. Remove one optional skill so the
# e2e proves cataloged-but-disabled packages are not projected.
awk '$0 != "  - principal-architect" { print }' \
  "${WORKSPACE}/.foundry/skills/enabled.yaml" > "${TMP_ROOT}/enabled.yaml"
mv "${TMP_ROOT}/enabled.yaml" "${WORKSPACE}/.foundry/skills/enabled.yaml"

COMMON=(-root "${REPO_ROOT}" -enabled "${WORKSPACE}/.foundry/skills/enabled.yaml")
"${BIN}" agents validate "${COMMON[@]}" > "${EVIDENCE_ROOT}/validate.txt"
"${BIN}" skills validate "${COMMON[@]}" >> "${EVIDENCE_ROOT}/validate.txt"

snapshot_tree "${WORKSPACE}" "${EVIDENCE_ROOT}/before.tree"
snapshot_digests "${WORKSPACE}" "${EVIDENCE_ROOT}/before.sha256"
snapshot_authority "${EVIDENCE_ROOT}/authority-before.sha256"

echo "==> install enabled agents and skills"
"${BIN}" agents install "${COMMON[@]}" -workspace "${WORKSPACE}" > "${EVIDENCE_ROOT}/install-first.txt"
"${BIN}" skills install "${COMMON[@]}" -workspace "${WORKSPACE}" >> "${EVIDENCE_ROOT}/install-first.txt"
snapshot_tree "${WORKSPACE}" "${EVIDENCE_ROOT}/after-first.tree"
snapshot_digests "${WORKSPACE}" "${EVIDENCE_ROOT}/after-first.sha256"

test -f "${WORKSPACE}/.claude/agents/planning.md"
test -f "${WORKSPACE}/.claude/skills/guardrails/SKILL.md"
if test -e "${WORKSPACE}/.claude/skills/principal-architect"; then
  echo "FAIL: disabled principal-architect skill was materialized" >&2
  exit 1
fi

echo "==> reinstall and compare byte-for-byte"
"${BIN}" agents install "${COMMON[@]}" -workspace "${WORKSPACE}" > "${EVIDENCE_ROOT}/install-second.txt"
"${BIN}" skills install "${COMMON[@]}" -workspace "${WORKSPACE}" >> "${EVIDENCE_ROOT}/install-second.txt"
snapshot_tree "${WORKSPACE}" "${EVIDENCE_ROOT}/after-second.tree"
snapshot_digests "${WORKSPACE}" "${EVIDENCE_ROOT}/after-second.sha256"
cmp "${EVIDENCE_ROOT}/after-first.tree" "${EVIDENCE_ROOT}/after-second.tree"
cmp "${EVIDENCE_ROOT}/after-first.sha256" "${EVIDENCE_ROOT}/after-second.sha256"
cmp "${EVIDENCE_ROOT}/install-first.txt" "${EVIDENCE_ROOT}/install-second.txt"

echo "==> doctor installed projection"
"${BIN}" agents doctor "${COMMON[@]}" -workspace "${WORKSPACE}" > "${EVIDENCE_ROOT}/doctor-green.txt"
"${BIN}" skills doctor "${COMMON[@]}" -workspace "${WORKSPACE}" >> "${EVIDENCE_ROOT}/doctor-green.txt"

echo "==> doctor detects a deleted managed file"
rm "${WORKSPACE}/.claude/agents/planning.md"
set +e
"${BIN}" agents doctor "${COMMON[@]}" -workspace "${WORKSPACE}" > "${EVIDENCE_ROOT}/doctor-missing.txt" 2>&1
doctor_status=$?
set -e
printf 'exit=%d\n' "${doctor_status}" >> "${EVIDENCE_ROOT}/doctor-missing.txt"
if [[ "${doctor_status}" -eq 0 ]]; then
  echo "FAIL: doctor accepted a missing managed file" >&2
  exit 1
fi

snapshot_authority "${EVIDENCE_ROOT}/authority-after.sha256"
cmp "${EVIDENCE_ROOT}/authority-before.sha256" "${EVIDENCE_ROOT}/authority-after.sha256"
if test -e "${WORKSPACE}/.git"; then
  echo "FAIL: packaging created SCM metadata" >&2
  exit 1
fi
printf '%s\n' \
  'PASS: executor/policy allowlists, internal/scm, and deploy are byte-identical' \
  'PASS: product workspace contains no SCM metadata' \
  'PASS: disabled principal-architect skill is absent' \
  > "${EVIDENCE_ROOT}/authority-proof.txt"

echo "==> product_packaging e2e: PASS"
