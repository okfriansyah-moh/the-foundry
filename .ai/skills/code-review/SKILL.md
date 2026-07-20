# Purpose

A general, multi-pillar review pass usable on any diff in this repo — broader than `task-review` (which is
PLAN-card-compliance-first). Use this when reviewing a PR, a remediation diff, or code from outside the
task-card flow (e.g. a dependency bump, a hotfix).

# The seven pillars

1. **Correctness** — does it do what it claims; are edge cases, error paths, and concurrency (goroutine leaks,
   races) handled; run `go test -race -count=1 ./...`.
2. **Security** — apply `security-hardening` (OWASP Top 10) and, if the diff touches an executor/adapter/PEC
   path, `ai-vulnerability-defense` (OWASP LLM Top 10).
3. **Architecture** — package boundaries and authority boundaries respected; cross-check the dispatched agent's
   `Boundaries` in `.ai/agents/<role>/AGENT.md`.
4. **Complexity** — apply `code-quality` (KISS/YAGNI/DRY/SOLID, complexity budget).
5. **Tests** — new behavior and regressions covered; deterministic (no flaky sleeps/timing races); coverage
   didn't drop on touched packages.
6. **Honesty of the report** — apply `stop-ai-slop`: every claim in the PR description matches something that
   was actually run and observed.
7. **Release readiness** — build, vet, staticcheck, `govulncheck`, and (per `docs/PLAN.md` §C) reversible
   migrations with a tested `down`.

# Findings Format

`[<severity>] <file:line> — <finding> → <exact fix>` — severities: `blocker`, `major`, `minor`, `nit`. Order by
severity, most severe first. No prose filler; see `.ai/prompts/pr-remediation.md`.

# Process

- Read the full diff before forming an opinion on any single line — a change that looks wrong in isolation may
  be correct given a sibling change elsewhere in the same diff.
- For anything touching `internal/kernel`, `internal/scm/write`, or `internal/pec`, review with Rev R3 rigor
  regardless of what the PR description claims the risk is — the task card's `Risk`/`Rev` fields are
  authoritative (Constitution C6), but a reviewer who spots a card that under-classified its own risk should say
  so, not silently apply the card's (wrong) tier.
- If the author and reviewer are the same session/agent instance for an R3/R4-rated change, say so explicitly —
  reviewer-independence (R0–R4) is violated, not satisfied, by a thorough self-review.

# Anti-Patterns

- Nitpicking style the linter already enforces instead of looking for correctness/security issues.
- Approving because "tests pass" without reading what the tests actually assert.
- A review that's longer than the diff and still doesn't cite a file:line.
