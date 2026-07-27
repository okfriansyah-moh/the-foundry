---
name: lint-final-check
description: "Use when a task is otherwise complete and you need the final repo-wide golangci-lint gate; run it at the very end, fix any errcheck/staticcheck/gofumpt findings in current-scope files, and rerun validation if edits were made."
---

# Purpose

Run the final golangci-lint pass after implementation, review, and repo-wide validation, then fix any remaining findings before reporting done.

# Process

1. Run `docker compose -f deploy/docker-compose.yaml run --rm dev golangci-lint run ./...`.
2. Fix only findings in the current task's scope.
3. Prefer minimal cleanup patterns: `_ = x.Close()`, checked `fmt.Fprintf`, and direct gofumpt formatting.
4. If you edit files, rerun golangci-lint and the task's validation commands before reporting.

# Anti-Patterns

- Deferring lint until after the report.
- Adding broad exclusions instead of fixing the finding.
- Touching unrelated files while chasing a final lint pass.