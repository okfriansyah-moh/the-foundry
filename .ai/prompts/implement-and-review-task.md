# Implement And Review Task

## Use

Implement, self-review, fix, and report Task `{{TASK_NUMBER}}` from `docs/PLAN.md` in one session.

## Inputs

- `AGENTS.md` or `CLAUDE.md` (composed provider artifact — whichever the invoking tool reads)
- `docs/architecture.md`
- `docs/PLAN.md`
- `.ai/skills/task-implementation/SKILL.md`
- `.ai/skills/task-review/SKILL.md`
- `.ai/skills/lint-final-check/SKILL.md`
- `.ai/agents/<role>/AGENT.md` for the card's `Exec` role — its `## Uses` section names every other skill this
  task needs (coding standards, code quality, security hardening, AI-vulnerability defense, QA, frontend/UI-UX);
  see `docs/architecture.md`'s Skill Catalog for the full roster.
- `.ai/prompts/pr-remediation.md`
- Task number: `{{TASK_NUMBER}}`

## Instructions

1. Read the inputs. Focus only on Task `{{TASK_NUMBER}}`.
2. Implement Task `{{TASK_NUMBER}}` using `.ai/skills/task-implementation/SKILL.md` plus every skill listed in
   the dispatched agent's `## Uses` section.
3. Self-review using `.ai/skills/task-review/SKILL.md`: PLAN compliance, constitution articles named in the card,
   architecture/authority boundaries (cross-check against the dispatched agent's boundaries), tests, security
   (OWASP Top 10 / OWASP LLM Top 10), complexity, coding standards, release readiness.
4. For each finding, apply `.ai/prompts/pr-remediation.md`: terse classification, exact fix, no filler.
5. Fix findings immediately when they are in Task `{{TASK_NUMBER}}` scope. Do not implement future tasks.
6. Run the task's Validation commands, then repo-wide `make test && make fitness`.
7. At the very end, run `.ai/skills/lint-final-check/SKILL.md` as the final CI-parity gate:
   - rerun repo-wide `golangci-lint` and fix in-scope findings,
   - run CI-parity validation (`make bootstrap test lint fitness`),
   - if `.ai/` files changed, recompose via `ars compose --target codex` and `ars compose --target claude` then rerun doclint/fitness,
   - if a PR exists, verify required checks are green before marking done.
     If this step changes files, rerun the task's Validation commands and `make test && make fitness` before moving on.
8. Report changed files, fixes made, validation results, skipped commands, and blockers.
9. Flip `Status: ☐ Not started` to `Status: ✅ <date>` for Task `{{TASK_NUMBER}}` and check its box in the §D
   Master Index of `docs/PLAN.md`.

## Check

- Single task only.
- Every path referenced by this prompt or the task's Outputs exists.
- Findings fixed or explicitly classified out of scope.
- Validation run after fixes.
- Output is concise and action-focused.
