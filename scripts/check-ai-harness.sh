#!/usr/bin/env bash
# Standalone reproducibility check for the ARES-canonical .ai/ harness (Task 2).
# Retired once Task 37 absorbs this into the real `make fitness` suite.
set -euo pipefail

fail=0

echo "== check-ai-harness: required paths exist =="
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

echo "== check-ai-harness: six AGENT.md files each name a constitution article =="
for f in .ai/agents/*/AGENT.md; do
  if ! grep -qE 'C([1-9]|1[0-9]|2[0-2])\b' "$f"; then
    echo "NO CONSTITUTION ARTICLE CITED: $f"
    fail=1
  fi
done

echo "== check-ai-harness: docs/foundry internal links resolve =="
if ! python3 - <<'PYEOF'; then
import re, os, sys

root = "docs/foundry"
link_re = re.compile(r'\]\(([^)]+)\)')
broken = False

for dirpath, _, files in os.walk(root):
    for fn in files:
        if not fn.endswith('.md'):
            continue
        path = os.path.join(dirpath, fn)
        with open(path, encoding='utf-8') as f:
            content = f.read()
        for m in link_re.finditer(content):
            link = m.group(1)
            if link.startswith(('http://', 'https://', 'mailto:', '#')):
                continue
            clean = link.split('#')[0]
            if not clean:
                continue
            target = os.path.normpath(os.path.join(dirpath, clean))
            if not os.path.exists(target):
                print(f"BROKEN: {path} -> {link}")
                broken = True

sys.exit(1 if broken else 0)
PYEOF
  fail=1
fi

echo "== check-ai-harness: ars validate =="
if command -v ars >/dev/null 2>&1; then
  if ! ars validate --root . --json; then
    echo "ars validate FAILED"
    fail=1
  fi
else
  echo "SKIPPED: ars not on PATH (expected inside dev image once Dockerfile.dev is rebuilt)"
fi

echo "== check-ai-harness: golden-rule reproducibility (delete + ars compose == byte-identical) =="
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
      echo "$g NOT reproducible from .ai/"
      fail=1
    fi
  done
  rm -rf "$tmp"
else
  echo "SKIPPED: ars not on PATH (expected inside dev image once Dockerfile.dev is rebuilt)"
fi

if [ "$fail" -ne 0 ]; then
  echo "check-ai-harness FAILED"
  exit 1
fi

echo "check-ai-harness OK"
