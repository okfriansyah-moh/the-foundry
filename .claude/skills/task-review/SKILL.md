---
name: task-review
description: "Review a completed task implementation for compliance with the plan, the constitution, architecture, tests,"
---

<!-- ars:source .ai/skills/task-review/SKILL.md -->
# Purpose

Review a completed task implementation for compliance with the plan, the constitution, architecture, tests,
security, complexity, and release readiness.

# Review Checklist

- **PLAN compliance:** only the selected task's Scope was implemented; every listed Output exists; no future task
  was pulled forward; the card's own Validation commands were run and passed.
- **Constitution articles:** identify every article the card names (`docs/PLAN.md` §B, C1–C22) and verify the
  change does not violate it — not a paraphrase check, a literal one.
- **Architecture / authority boundaries:** cross-check the change against the _dispatched agent's_ `Boundaries`
  section in `.ai/agents/<role>/AGENT.md` — e.g. a `go-backend` task touching `internal/scm/write` is an automatic
  fail regardless of what the diff otherwise looks like.
- **Tests:** apply `.ai/skills/qa-testing/SKILL.md` — new behavior, edge cases, and regressions are covered; the
  task's Validation commands are re-runnable and green.
- **Security:** apply `.ai/skills/security-hardening/SKILL.md` (OWASP Top 10) and, for anything touching an
  executor/adapter/PEC path, `.ai/skills/ai-vulnerability-defense/SKILL.md` (OWASP LLM Top 10) — no secrets
  committed, no unscoped side effects, no widened network/egress without a matching row in the container
  topology table.
- **Complexity:** apply `.ai/skills/code-quality/SKILL.md` — no speculative abstraction, no premature
  generalization beyond what the card's Steps required.
- **Style:** apply `.ai/skills/coding-standards/SKILL.md`.
- **Honesty:** apply `.ai/skills/stop-ai-slop/SKILL.md` — every claim in the implementer's report matches
  something actually run and observed in-session.
- **Release readiness:** `make test && make fitness` green; Status line and Master Index checkbox updated.

For a broader, non-task-card review (a PR, a remediation diff, a dependency bump), use
`.ai/skills/code-review/SKILL.md` instead — it runs the same pillars without assuming a PLAN task card exists.

# Findings Format

`[<severity>] <file:line> — <finding> → <exact fix>` (see `.ai/prompts/pr-remediation.md`) — no prose filler.

# Anti-Patterns

- Approving a diff you also authored in the same session for a Rev R3/R4 task — reviewer independence (R0–R4)
  requires an independent pass, not a self-check standing in for one.
- Reviewing style while missing a constitution or authority-boundary violation.
- Accepting untested behavior because the change is small.
- Recommending broad refactors unrelated to the selected task.
