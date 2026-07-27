---
name: lint-final-check
description: "Run the final CI-parity gate after implementation/review so local completion matches CI outcomes, then report done only after required checks are green."
---

<!-- ars:source .ai/skills/lint-final-check/SKILL.md -->
---
name: lint-final-check
description: "Use when a task is otherwise complete and you need final CI-parity gating; run golangci-lint at the end, fix findings in-scope, rerun validation, and enforce .ai composed-file reproducibility and PR checks before reporting done."
---

# Purpose

Run the final CI-parity gate after implementation/review so local completion matches CI outcomes, then report done only after required checks are green.

# Process

1. Run `docker compose -f deploy/docker-compose.yaml run --rm dev golangci-lint run ./...`.
2. Fix only findings in the current task's scope.
3. Prefer minimal cleanup patterns: `_ = x.Close()`, checked `fmt.Fprintf`, and direct gofumpt formatting.
4. Run CI-parity local validation: `make bootstrap test lint fitness`.
5. If any edited path is under `.ai/`, recompose provider artifacts before final validation:
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target codex`
   - `docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target claude`
   - Then rerun `make bootstrap doclint` (or `make bootstrap test lint fitness`).
6. If a PR exists for the branch, verify checks are green before reporting done:
   - `gh pr checks <number> --required`
   - Optionally `gh pr checks <number> --watch` until completion.
7. If any step in this process edits files, rerun the relevant validation commands before reporting.

# Anti-Patterns

- Deferring lint until after the report.
- Adding broad exclusions instead of fixing the finding.
- Touching unrelated files while chasing a final lint pass.
- Editing `.ai/` content without recomposing `AGENTS.md`/`CLAUDE.md` and generated skill trees.
- Marking task done while required PR checks are failing or still pending.
