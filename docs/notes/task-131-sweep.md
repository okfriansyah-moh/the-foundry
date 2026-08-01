# Task 131 (DOC-01) — comment sweep evidence

Sweep date: 2026-08-01. Scope: every `TODO`, `future task`, `not yet wired`,
`pending Task N`, `STUB`, and `not-yet-built` comment under `internal/` and
`cmd/`.

## Corrected (referenced task had landed)

| File | Was | Action |
| --- | --- | --- |
| `internal/recovery/supervisor.go` | "whichever future task wires … foundryd" | Updated: Task 94 delivered PostgresProjectionSource + foundryd loop |
| `internal/recovery/postgres.go` | deferred out of Task 32 | Updated: Task 94 wiring note |
| `internal/kernel/scmpush.go` | Task 28 "will make"; branch policy "not-yet-built" | Updated: Task 28 enforced; Task 108 policy + TenXDeliver path |
| `internal/scm/write/doc.go` | "Task 35 has not landed" | Updated: Task 35 + Task 137 secrets path |
| `internal/scm/write/secrets.go` | "Task 35 doesn't exist yet" | Updated: Task 35 + Task 137 secrets path |
| `internal/mission/evaluator.go` | "Task 49 doesn't exist yet" / future Task 49 | Updated: Task 49 interface documented without false pending claim |
| `internal/mission/workflow.go` | "Tasks 41–44 … none exist yet" | Updated: pipeline delivered |
| `internal/kernel/budget.go` | "a future task" (pricing registry) | Updated: names Task 120 |
| `internal/ledger/cost/defaults.go` | "a future task" (plan cost field) | Rephrased: later plan-schema task (no numbered card yet) |
| `cmd/foundry/policy.go` | "a future task wiring org-policy" | Rephrased: no false pending-task claim |
| `internal/kernel/doc.go` | (already accurate) | No change — documents Tasks 13/99/104 validation path |
| `test/helpers/startplan/main.go` | (already accurate) | No change — Task 105 lane resolution documented |
| `internal/executor/claudecode/adapter.go` | (already accurate) | No change — Task 115 sandbox seam documented |

## Left open (still accurate; no completed task falsely cited)

| File | Comment summary | Closing task / note |
| --- | --- | --- |
| `internal/kernel/workflow.go` | future task can add per-task timeout from plan schema | plan-schema extension (unnumbered) |
| `internal/ledger/extops/store.go` | future task can add explicit reconciled state | ledger hardening (unnumbered) |
| `internal/provenance/audit.go` | future task can extract audit chain reader | projection/audit read API (unnumbered) |
| `internal/provenance/org.go` | Jira/TestRail stub with TODO | integration adapters (unnumbered) |
| `internal/policy/compiler/load.go` | future task for workflow layer source | workflow-definition task (unnumbered) |
| `internal/mission/workflow.go` | "Tasks 41–44 … none of which exist yet" | Updated: pipeline delivered (Tasks 41–44) |
| `internal/mission/evaluator.go` | not-yet-built integration points | mission evaluator wiring (unnumbered) |
| `internal/policy/pdp/decider.go` | `TODO(foundry): migrate … opa/v1` | dedicated OPA v1 migration (unnumbered) |
| `internal/bench/report.go` | "gate pending Task 135" | Task 135 ⬜ — accurate |

## Docs reconciliation (step 4b)

| File | Action |
| --- | --- |
| `docs/foundry/docs/workflows/multi-repository.md` | Annotated Bitbucket overclaims at Steps 4, 12, 13 (branch restrictions, PR listing, Pipelines) |

## Hygiene

| Item | Action |
| --- | --- |
| `internal/spec/mockup` RetentionRoot | Configurable absolute path; tests use `t.TempDir()` |
| `internal/spec/mockup/data/visual-inputs/visual-*` | Deleted committed fixture trees |
| `.gitignore` | Added `data/visual-inputs/` |

## New enforcement

| Rule | Seed |
| --- | --- |
| Stale future-task comment lint | `test/fitness_seeds/stale_task_comment/` |
| Test must not write package source tree | `test/fitness_seeds/test_source_write/` |
| Per-term banned-term list (prior org brand + superseded state alias) | `test/fitness_seeds/term/` |

## De-branding owner decision (out of scope per Task 131)

Commit author trailers may still carry the prior organization domain in git
metadata; history rewrite is explicitly out of scope. The banned brand term
and the superseded state alias are enforced by `fitlint term` with per-term
allowlists (seed: `test/fitness_seeds/term/`).
