# M0 Exit Report — Shared Kernel Proof (docs/PLAN.md Task 19 / SKP-17)

**Status: PENDING — live M0 exit proof not executed.** No Docker daemon has
been reachable in this development session (the same established blocker
recorded on every task's Status line from Task 2 through Task 18: Postgres
and Temporal are compose-network services with no host-installed
equivalent available here). `make skp-e2e` (`test/skp_e2e.sh`) is written,
wired into the Makefile, and traced logically against the actual CLI/
Temporal/Postgres surfaces this repo already has — but it has never been
run. Every evidence slot below is marked PENDING rather than filled with
invented run IDs, dates, or bundle hashes, per docs/PLAN.md §A ("no
self-reported done") and Constitution C10 (evidence-based completion).

This report will be appended-to automatically by `test/skp_e2e.sh`'s final
step the first time it runs successfully against live infra (see the
"## Run <pid> (<date>)" block that step writes) — that appended block, not
this hand-written section, is the real evidence.

## What `make skp-e2e` actually does (ready, unexecuted)

1. `foundry doctor` — Postgres + Temporal reachability.
2. Scenario **success**: one-task plan pointed at
   `test/fixtures/fake_scripts/success.yaml`, submitted/approved/started
   against the fake executor, polled to `SUCCEEDED`.
3. Scenario **deterministic-fail**: same shape, pointed at
   `test/fixtures/fake_scripts/fail.yaml`, polled to `FAILED`.
4. Scenario **resume**: delegates to `test/skp_resume_test.sh` (Task 16) —
   kill -9 mid-plan, restart, exactly-once receipt proof, plus its
   negative control. The 20x repetition proof is the dedicated
   `make skp-resume` CI job, not re-run 20x again inside `skp-e2e`.
5. `foundry evidence verify` on every bundle produced by steps 2–3.
6. Status consistency check: delegates to `test/status_consistency_e2e.sh`
   (Task 15) — projected vs. `--fresh` diverge under induced lag, converge
   after projector tick.
7. `bash scripts/fitness.sh` (Task 18).
8. Archive evidence bundles + `workflow_transitions` history CSV to
   `evidence/m0-exit/<run-id>/`.
9. Append a run block (workflow IDs, Temporal run IDs, archive paths) to
   this file.

## Evidence slots

| Item | Status |
| --- | --- |
| `make skp-e2e` full run (single command green from clean `make up`) | **PENDING** — requires live Docker/Postgres/Temporal, not available in this development session |
| Success plan run ID | **PENDING** |
| Deterministic-fail plan run ID | **PENDING** |
| Resume proof (single run) | **PENDING** |
| Resume proof (20x, `make skp-resume`) | **PENDING** — see docs/PLAN.md Task 16 Status line for the same blocker |
| `foundry evidence verify` results | **PENDING** |
| Status consistency check result | **PENDING** |
| `evidence/m0-exit/**` archive | **PENDING** — not created; this task deliberately does not fabricate bundle contents |
| Git tag `v0.1.0-skp` | **NOT CREATED** — tagging a release on unexecuted evidence would violate Constitution C10; deferred until a real `skp-e2e` run succeeds |

## Constitution articles touched in M0, and their proof status

| Article | What it requires | Owning task(s) | Proof status |
| --- | --- | --- | --- |
| C1 | Exactly six workflow statuses | `internal/state` (Task 5 et al.) | Unit-tested (`go test ./internal/state/...`) — green. Live status transitions through Temporal: PENDING |
| C2 | Temporal owns durable execution history/sequencing | `internal/kernel` workflow (Tasks 8, 12) | Workflow logic unit-tested via Temporal testsuite — green. Live Temporal execution: PENDING |
| C3 | Postgres projection is rebuildable, never authoritative | Task 14 (`internal/projection`) | Unit-tested — green. Live projector against real Postgres: PENDING |
| C4 | Kernel owns all side effects incl. SCM writes | `internal/kernel` (Tasks 8, 12) | Unit-tested (activities, worktree, evidence recording) — green. Live end-to-end side effects: PENDING |
| C6 | Admission classifier never self-authorizes | Task 12 (admission v0) | Unit-tested — green |
| C7 | ApprovedPlan provenance chain | Tasks 8–9 (submit/approve, Ed25519) | Unit-tested — green. Live `plan submit`/`plan approve` round trip against Postgres: PENDING |
| C8 | Isolated worktrees | Task 12 (`internal/worktree`) | Unit-tested against a real local git repo — green (no Docker needed for this specific check) |
| C9 | External-operation ledger + idempotency keys | Task 16 (receipts) | Unit-tested incl. the receipt-deletion negative control (`TestExecuteTask_WithoutReceiptGuardActuallyReRuns`) — green. Live receipts count assertion from `test/skp_resume_test.sh`: PENDING |
| C10 | Evidence-based completion; no self-reported done | Task 11 (`internal/evidence`), this task's own report | Unit-tested bundle put/get/verify/tamper-detection — green. This very report follows C10 by marking unexecuted work PENDING rather than fabricating it |
| C22 | Recovery/checkpoint/restart proof | Task 16 (`internal/recovery`) | Unit-tested (`Checkpoint.ID()`, workflow.go stamping) — green. Live kill-9/restart proof: PENDING |

## Precedent for this status marking

This report mirrors the honesty discipline already established on Tasks
12–18's Status lines in `docs/PLAN.md`: every one of those tasks passed its
repo-local validation (`go build`, `go vet`, `go test`, `gofmt`,
`scripts/fitness.sh`) but explicitly declined to claim its card's
live-infra Acceptance criterion as met. Task 19 is the task whose entire
purpose is to prove those criteria with live evidence — so it is held to
the same rule, not a laxer one just because it is the milestone-closing
task.
