---
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
   - Run each target separately in sequence: `make bootstrap`, then `make test`, then `make lint`, then `make fitness`, stopping if any fails.
   - If local commands pass but CI still fails, inspect environment differences (OS, Node/Python version, secrets, cache) and document the discrepancy under Remaining blockers.
3. Apply minimal fixes in root-cause files only.
   - After applying fixes, confirm that previously passing checks still pass. If a regression is detected, revert the change and document it as a Remaining blocker.
4. If `.ai/` files changed:
   - Run `make bootstrap doclint`.
   - Run `ars compose --target codex`.
   - Run `ars compose --target claude`.
   - Rerun `make fitness`.
5. Re-run validations until clean.
6. If a PR exists, verify required checks are green before done:
   - `gh pr checks <number> --required`

## Output

- Root cause summary per failing job
- Files changed
- Commands run with outcomes
- Remaining blockers (if any)
