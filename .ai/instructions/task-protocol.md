# Task Protocol

Summary of `docs/PLAN.md` §A — how to execute the plan with an AI agent.

## Default mode: orchestrator-driven

Once Task 3 (the autonomous PLAN runner) exists, it — not a human — is the default trigger and default
report-recipient for every task from Task 4 onward. The runner reads the Master Index, selects the next eligible
task (`Depends` all ✅), drives its implementation, and reports to itself: it updates the Index, appends to §T, and
moves to the next task.

- **Auto path** — `Risk: Low`/`Med` and `Rev: R1`/`R2` completes end-to-end with zero human steps; reported via a
  non-blocking batched Telegram digest.
- **Gated path** — `Risk: High` or `Rev: R3`/`R4` pauses before commit and sends a blocking Telegram message,
  waiting for `/approve` or `/reject`. No reply ⇒ stays paused; never auto-approves.

## Bootstrap exception

Tasks 1, 2, and 3 are necessarily human-triggered — nothing can orchestrate task selection before the orchestrator
exists. From Task 4 onward, let the runner drive.

## Manual protocol (required for Tasks 1–3; available anytime after as an explicit override)

1. `docs/PLAN.md` is the canonical plan location.
2. The trigger (human for Tasks 1–3, the runner after) gives the agent a task number.
3. The agent implements exactly the card: **Scope** is the allowed surface, **Out of scope** is forbidden
   (violations = review rejection even if tests pass), **Steps** are the ordered path, **Outputs** are exact paths
   that must exist when done.
4. The agent runs the task's **Validation** commands, then repo-wide `make test && make fitness`.
5. The agent reports to whichever party triggered it — changed files, fixes, validation results, skipped commands,
   blockers — flips `Status: ☐ Not started` to `Status: ✅ <date>`, and checks its box in the §D Master Index.
6. Any task whose `Depends` are all ✅ may start; `[P]`-marked tasks with disjoint Outputs may run concurrently,
   bounded to 2 at a time by default.

## No-gaps rule

If an agent hits a genuinely unspecified detail it must (a) check the task's Governing docs, (b) apply §B/§C
defaults, and only then (c) choose the smallest reversible option and record it in the task's Status line as
`decision: <what>`. Inventing scope is never allowed — this applies identically whether the trigger was a human or
the runner.
