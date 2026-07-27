---
mode: agent
description: "Fix failing CI checks with local CI-parity validation and minimal scoped changes."
---

# Fix CI

Goal: make all failing CI checks pass with the smallest safe diff.

## Inputs

- Current branch and PR number (if any)
- Failing check names or run links
- Relevant workflow files and changed source files

## Procedure

1. Identify failing jobs and failing steps from CI logs.
2. Reproduce locally with CI-parity commands:
   - `make bootstrap test lint fitness`
   - `make bootstrap doclint` when docs, `.ai/`, compose, Docker, or workflow files changed.
3. Apply minimal fixes in root-cause files only.
4. If `.ai/` files changed, recompose and rerun doclint/fitness:
   - `ars compose --target codex`
   - `ars compose --target claude`
5. Re-run validations until clean.
6. If a PR exists, verify required checks are green before done:
   - `gh pr checks <number> --required`

## Output

- Root cause summary per failing job
- Files changed
- Commands run with outcomes
- Remaining blockers (if any)
