#!/usr/bin/env bash
# docs/PLAN.md Task 37 (FND-18): composed-file-reproducibility lint. Absorbs
# Task 2's standalone scripts/check-ai-harness.sh into real CI (that script
# is retired now that this runs as part of `make fitness` / `make doclint` on
# every PR, not just once at Task 2 time).
#
# ARES's golden rule, made a literal, enforced gate: AGENTS.md and CLAUDE.md
# (plus the .agents/.claude/.codex provider directories `ars compose` also
# emits) are *composed*, never hand-edited, from .ai/. Deleting them and
# re-running `ars compose` for both targets must reproduce them
# byte-identical. Any difference — a hand-edit that drifted from .ai/, or a
# forgotten recompose after an .ai/ change — fails this check by name.
set -euo pipefail

cd "$(dirname "$0")/../.."

fail=0

echo "== doclint/ai-harness-repro: required .ai/ + composed-file paths exist =="
required_paths=(
  ".ai/manifest.yaml"
  ".ai/instructions/build-and-test.md"
  ".ai/instructions/authority-boundaries.md"
  ".ai/instructions/task-protocol.md"
  ".ai/instructions/prompt-caching.md"
  ".ai/agents/go-kernel/AGENT.md"
  ".ai/agents/go-backend/AGENT.md"
  ".ai/agents/integration/AGENT.md"
  ".ai/agents/infra/AGENT.md"
  ".ai/agents/web/AGENT.md"
  ".ai/agents/security-review/AGENT.md"
  ".ai/skills/task-implementation/SKILL.md"
  ".ai/skills/task-review/SKILL.md"
  ".ai/skills/coding-standards/SKILL.md"
  ".ai/skills/code-quality/SKILL.md"
  ".ai/skills/stop-ai-slop/SKILL.md"
  ".ai/skills/security-hardening/SKILL.md"
  ".ai/skills/ai-vulnerability-defense/SKILL.md"
  ".ai/skills/code-review/SKILL.md"
  ".ai/skills/qa-testing/SKILL.md"
  ".ai/skills/frontend-development/SKILL.md"
  ".ai/skills/ui-ux-design/SKILL.md"
  ".ai/prompts/implement-and-review-task.md"
  ".ai/prompts/pr-remediation.md"
  "docs/architecture.md"
  "docs/PLAN.md"
  "docs/foundry/delivery_foundry.md"
  "AGENTS.md"
  "CLAUDE.md"
)
for p in "${required_paths[@]}"; do
  if [ ! -f "$p" ]; then
    echo "MISSING: $p"
    fail=1
  fi
done

echo "== doclint/ai-harness-repro: six AGENT.md files each name a constitution article =="
for f in .ai/agents/*/AGENT.md; do
  if ! grep -qE 'C([1-9]|1[0-9]|2[0-2])\b' "$f"; then
    echo "NO CONSTITUTION ARTICLE CITED: $f"
    fail=1
  fi
done

echo "== doclint/ai-harness-repro: ars validate =="
if command -v ars >/dev/null 2>&1; then
  if ! ars validate --root . --json; then
    echo "ars validate FAILED"
    fail=1
  fi
else
  echo "SKIPPED: ars not on PATH (expected inside dev image — deploy/Dockerfile.dev installs it)"
fi

echo "== doclint/ai-harness-repro: golden-rule reproducibility (delete + ars compose == byte-identical) =="
if command -v ars >/dev/null 2>&1; then
  tmp="$(mktemp -d)"
  generated=(AGENTS.md CLAUDE.md .agents .claude .codex)
  for g in "${generated[@]}"; do
    [ -e "$g" ] && cp -r "$g" "$tmp/$(basename "$g").before"
  done
  rm -rf "${generated[@]}"
  ars compose --target codex --root . >/dev/null
  ars compose --target claude --root . >/dev/null
  for g in "${generated[@]}"; do
    before="$tmp/$(basename "$g").before"
    [ -e "$before" ] || continue
    if ! diff -rq "$before" "$g" >/dev/null 2>&1; then
      echo "$g NOT reproducible from .ai/ (hand-edited or .ai/ change not recomposed)"
      fail=1
    fi
  done
  rm -rf "$tmp"
else
  echo "SKIPPED: ars not on PATH (expected inside dev image — deploy/Dockerfile.dev installs it)"
fi

if [ "$fail" -ne 0 ]; then
  echo "doclint/ai-harness-repro FAILED"
  exit 1
fi

echo "doclint/ai-harness-repro OK"
