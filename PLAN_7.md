# PLAN.md — Delivery Foundry Implementation Plan

**Plan version:** 2.0 (AI-ready, sequentially numbered) · **Date:** 2026-07-19 · **Tasks:** Task 1 → Task 83 · **Start at: Task 1.**
**Source of truth:** Delivery Foundry V12 documentation set (`delivery_foundry.md` + `docs/**`, vendored into the repo by Task 2).
**Planning discipline:** GitHub Spec Kit (constitution gates, dependency-ordered tasks, `[P]` parallel markers, checkpoints) layered on top of the V12 architecture — never replacing it.

---

## A. How to execute this plan with an AI agent

**Default mode is orchestrator-driven, not human-driven.** Once Task 3 (the autonomous PLAN runner) exists, it — not you — is the default trigger and the default report-recipient for every task from Task 4 onward. The runner reads this file's Master Index, selects the next eligible task (`Depends` all ✅), drives its implementation, and reports to itself: it updates the Index, appends to §T, and moves to the next task. Human involvement is minimized to a Telegram gate, and only for the tasks this plan already marks as deserving one:

- **Auto path** — a task whose own card says `Risk: Low` or `Med` **and** `Rev: R1` or `R2` completes end-to-end with zero human steps. You learn about it from a non-blocking Telegram digest (batched every 5 completions or 2 hours, whichever comes first) — the same shape as the venture loop's weekly veto digest. It never blocks progress.
- **Gated path** — a task whose own card says `Risk: High` **or** `Rev: R3`/`R4` pauses before commit and sends a blocking Telegram message (task, changed files, validation results, and the exact reason it's gated), waiting for `/approve` or `/reject`. No reply within the configured window ⇒ the runner stays paused; it never auto-approves.

This reuses the product's own A0/A1/A2/H admission-tier logic (`docs/foundry/docs/autonomy/admission-tiers.md`) applied to _building_ the product — using data every card already carries (`Risk`/`Rev`), so no separate classifier is needed. See Task 3 for the runner itself, including the exit condition under which this bootstrap tool retires once Foundry can run its own backlog directly.

**Bootstrap exception — Tasks 1, 2, and 3 are necessarily human-triggered.** Nothing can orchestrate task selection before the orchestrator exists. Trigger these three manually, below. From Task 4 onward, let the runner drive; your only touchpoints are its Telegram gates.

**Manual protocol** (required for Tasks 1–3; available anytime after as an explicit override):

1. This file lives at **`docs/PLAN.md`** in the `delivery-foundry` repository (Task 2 puts it there).
2. Give the agent a task number `{{TASK_NUMBER}}` — you for Tasks 1–3, the runner for everything after.
3. The agent implements exactly the card: **Scope** is the allowed surface, **Out of scope** is forbidden (violations = review rejection even if tests pass), **Steps** are the ordered path, **Outputs** are exact paths that must exist when done.
4. The agent runs the task's **Validation** commands, then repo-wide `make test && make fitness`.
5. The agent reports — changed files, fixes, validation results, skipped commands, blockers — **to whichever party triggered it**, not by default to a human once the runner exists; flips `Status: ☐ Not started` to `Status: ✅ <date>`; checks its box in the §D Master Index.
6. Any task whose _Depends_ are all ✅ may start; `[P]`-marked tasks with disjoint Outputs may run concurrently, bounded to 2 at a time by default. Numbers are stable names — **dependencies, not numbers, are the execution-order authority** (Tracks A and B interleave freely after Task 39).

**No-gaps rule:** if an agent hits a genuinely unspecified detail it must (a) check the task's Governing docs, (b) apply §B/§C defaults, and only then (c) choose the smallest reversible option and record it in the task's Status line as `decision: <what>`. Inventing scope is never allowed — this applies identically whether the trigger was you or the runner.

---

## B. Constitution — non-negotiable articles (gate every task)

| #   | Article                                                                                                                                                        | Enforced by     |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- |
| C1  | Exactly six workflow statuses (`PENDING RUNNING WAITING SUCCEEDED FAILED CANCELLED`); all richer meaning in registry-controlled `phase`/`reason`/`result_code` | Task 5, 18      |
| C2  | Temporal owns durable execution history, timers, sequencing                                                                                                    | Task 12         |
| C3  | PostgreSQL workflow state = rebuildable projection, never execution authority                                                                                  | Task 14, 38     |
| C4  | Kernel owns sequencing, retries, leases, fencing, state, policy, budgets, **all side effects incl. SCM writes**                                                | Task 12, 27, 28 |
| C5  | PEC only proposes waves/dispatch/remediation; prohibition-tested                                                                                               | Task 56         |
| C6  | Deterministic versioned AdmissionClassifier; a plan can never classify or authorize itself                                                                     | Task 7, 45      |
| C7  | ApprovedPlan provenance chain; authorship = provenance, never authorization                                                                                    | Task 8, 24      |
| C8  | Isolated worktrees; agents never touch canonical clones                                                                                                        | Task 9          |
| C9  | External-operation ledger + idempotency keys for every side effect                                                                                             | Task 26         |
| C10 | Evidence-based completion; no self-reported done                                                                                                               | Task 11, 13     |
| C11 | Telegram = notify/batch/flood-control/low-risk commands/veto digest; **never** high-risk approval                                                              | Task 30, 52     |
| C12 | Strong auth (OIDC+WebAuthn) for high-risk approvals                                                                                                            | Task 25         |
| C13 | `personal-autonomous-venture` profile = explicit bounded production-auto grant                                                                                 | Task 47         |
| C14 | Organization/10x profile = stricter governance + provenance                                                                                                    | Task 54, 55     |
| C15 | 10x terminal `status: SUCCEEDED, result_code: TEN_X_BRANCH_HANDOFF_READY`; **no PR/merge/staging/deploy in that workflow**                                     | Task 60, 61     |
| C16 | Mockup = first-class entry with Observed/Inferred/Assumed/Unresolved labels                                                                                    | Task 43         |
| C17 | Mission Setup Ceremony precedes unattended missions                                                                                                            | Task 41         |
| C18 | Formal mission success/loop-exit semantics (result codes)                                                                                                      | Task 40         |
| C19 | Cost accounting reserve→incur→reconcile; budgets enforced pre-execution; per-session caps                                                                      | Task 29, 69     |
| C20 | CumulativeChangeBudget governs autonomous L0/L1 promotion                                                                                                      | Task 75         |
| C21 | Synthetic verification below canary threshold, honestly labeled                                                                                                | Task 48         |
| C22 | Recovery/checkpoint/restart + honest `PROVEN_BLOCKED` on FAILED                                                                                                | Task 16, 32     |

**Constitution Check (every task + milestone exits):** `make fitness` = enum lint, superseded-term lint, import-boundary checks, PEC prohibitions (once present), payload limits, doc-link check. Zero tolerance at milestone exits (Tasks 19, 39, 53, 63, 73).

---

## C. Conventions (defaults every task inherits)

- **Stack:** Go 1.22+, Temporal (dev server → self-hosted), PostgreSQL 16, OPA PDP, Telegram Bot API, Playwright, Stripe test mode, Fly.io personal deploys (Blocker B1), GitHub-first SCM (Blocker B2). **All of the above runs inside the `dev` Docker image (Task 1) — the host needs only Docker + GNU make.**
- **Docker execution model:** every `make <target>` is a thin wrapper around `docker compose run --rm dev <real command>` (long-running services use `up`/`down` instead). **Target names and their meaning never change** — only the implementation is containerized, so every Validation command in every task card below (`make test`, `go test ./...`, `bash test/foo.sh`, etc.) is run the same way whether typed directly or through `make`; commands shown as bare `go test`/`go run`/`bash` are understood to execute inside `dev` (either via a `make` target or `docker compose run --rm dev <cmd>` directly — an agent may use either form). Host requirements: Docker Engine/Desktop + Docker Compose v2 + GNU make — no local Go, Node, Playwright, or database install is ever required. CI builds and runs the identical `dev` image (dev/CI parity — no "works on my machine"). Go module and build caches live in named Docker volumes so repeated runs stay fast.
- **Container topology & network policy (single source of truth — anti-sprawl rule):** exactly four image lineages exist for the life of this plan, each with one owner task and one stated purpose. **No fifth image or second compose file may be added without a matching row here** — Task 37 lints for it, so an ad hoc `Dockerfile.whatever` fails CI, not just code review.

  | Image                        | Owner   | Purpose                                                     | Lifecycle                                                                         | Network                                                                                                                       |
  | ---------------------------- | ------- | ----------------------------------------------------------- | --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
  | `dev`                        | Task 1  | toolchain to build/test/run Foundry itself                  | long-running compose service                                                      | full outbound internet (Docker's default) — needed for module/package fetches, GitHub, Anthropic, Stripe, Fly, Telegram, OIDC |
  | `postgres`, `temporal`       | Task 4  | `dev`'s runtime dependencies                                | long-running compose services, same file as `dev`                                 | internal compose network only; no outbound needed                                                                             |
  | `foundry-executor-sandbox`   | Task 34 | isolates AI-agent-executed task code                        | ephemeral, one per task execution, spawned by kernel Go code — **not** in compose | default-deny egress + narrow explicit allowlist (least privilege — see Task 34)                                               |
  | product template's own image | Task 46 | the venture product's own runtime, built/deployed by Fly.io | belongs to the generated product repo, not the platform                           | governed by the product, not by Foundry                                                                                       |
  | `foundry` (release)          | Task 73 | the shipped `foundry`/`foundryd` binaries                   | versioned release artifact                                                        | not applicable — not run as a dev/CI container                                                                                |

  Two hard rules, not preferences: (1) `deploy/docker-compose.yaml` holds only the long-running dev-time services (`dev`, `postgres`, `temporal`) — one file, never a second. (2) The network default is **open outbound everywhere** — nobody "hardens" `dev`, `postgres`, or `temporal` by restricting egress, because that only breaks builds without buying security (they never run untrusted code). The **executor sandbox is the sole deliberate exception**: default-deny, because it's the one place potentially-arbitrary agent-generated code executes. Per C4, that code never needs to reach GitHub, Stripe, or Fly directly — SCM writes, deployments, and billing are kernel-side activities that happen outside the sandbox — so its real legitimate need is narrow: the configured executor's own LLM provider endpoint, and nothing else by default (made concrete in Task 34, including a _positive_ test that the allowlist actually grants that access, not only that it denies everything else).

- **Task card fields:** Goal · Rationale · Depends · Governing docs · Scope · Out of scope · Steps · Outputs (exact paths) · Interfaces/DB (when relevant) · Acceptance (testable) · Validation (exact commands) · Evidence · Risk · Exec role · Review level (R1–R4 per `docs/security/reviewer-independence.md`; deterministic checks override LLM review) · Completion boundary · Status.
- **Defaults:** evidence = validation output + coverage + bundle archived to `evidence/task-<N>/`; completion boundary = merged to `main`, CI green, nothing outside listed Outputs; migrations reversible (`down` tested in CI); every new package has `doc.go` stating its authority limits; secrets never committed; all timestamps UTC; errors wrapped `%w`; table names snake_case; Go packages lower-case single word.
- **Executor roles:** `go-kernel` (authority-bearing), `go-backend`, `integration`, `infra`, `web`, `security-review`.
- **Risk⇒Review:** High ⇒ R3 minimum (independent fresh session + deterministic checks); security-surface High in M2 ⇒ R4.
- **Git:** branch `task/<N>-<slug>`; conventional commits; footer `Task: <N>` on every commit.
- **Make targets contract (created Task 1, extended by later tasks; never renamed):** `bootstrap up down doctor test lint fitness skp-e2e e2e-github e2e-venture e2e-tenx evidence-verify projection-rebuild`. Each wraps `docker compose run --rm dev <cmd>` (or `up`/`down`); adding a target never changes an existing one's name or docker-wrapping pattern.

---

## D. Master Task Index (the checklist your agent updates)

Legend: `[P]` = parallel-safe within its wave once Depends are ✅. M0=SKP, M1=Foundation, A=Venture, B=10x, M2=Hardening, M3=Evolution. **Dependencies are authoritative; numbers are names.**

| ✔   | Task | Alias  | Title                                                           | Phase/Wave | Depends                    | [P]  |
| --- | ---- | ------ | --------------------------------------------------------------- | ---------- | -------------------------- | ---- |
| ☐   | 1    | HAR-01 | Repository scaffold, Docker-wrapped Makefile, CI                | M0/S0      | —                          | None |
| ☐   | 2    | HAR-02 | Agent harness (AGENTS.md, .ai skills, docs vendored)            | M0/S0      | 1                          | [P]  |
| ☐   | 3    | RUN-01 | Autonomous PLAN runner: risk-tiered orchestrator, Telegram gate | M0/S0      | 2                          | [P]  |
| ☐   | 4    | SKP-02 | Runtime services in compose: Temporal+PG, make up/doctor        | M0/S0      | 1                          | [P]  |
| ☐   | 5    | SKP-03 | Canonical state package (C1)                                    | M0/S0      | 1                          | [P]  |
| ☐   | 6    | SKP-04 | PLAN schema, parser, canonical digest                           | M0/S0      | 1                          | [P]  |
| ☐   | 7    | SKP-05 | Deterministic AdmissionClassifier v0 (C6)                       | M0/S1      | 6                          | None |
| ☐   | 8    | SKP-06 | Signed ApprovedPlan provenance v0 (C7)                          | M0/S1      | 6,4                        | None |
| ☐   | 9    | SKP-07 | Worktree manager (C8)                                           | M0/S1      | 1                          | [P]  |
| ☐   | 10   | SKP-08 | Executor contract + fake executor                               | M0/S1      | 1                          | [P]  |
| ☐   | 11   | SKP-09 | Evidence bundle + FS object store (C10)                         | M0/S1      | 1                          | [P]  |
| ☐   | 12   | SKP-10 | Kernel workflow on Temporal (C2,C4)                             | M0/S2      | 5,7,8,9,10,11              | None |
| ☐   | 13   | SKP-11 | Deterministic validation runner (C10)                           | M0/S2      | 10                         | [P]  |
| ☐   | 14   | SKP-12 | Status projection v0 (C3)                                       | M0/S2      | 12                         | [P]  |
| ☐   | 15   | SKP-13 | CLI status with consistency levels                              | M0/S2      | 14                         | [P]  |
| ☐   | 16   | SKP-14 | Checkpoint + forced-restart resume proof (C22)                  | M0/S3      | 12,13                      | None |
| ☐   | 17   | SKP-15 | Claude Code executor adapter (flagged)                          | M0/S3      | 10,12                      | [P]  |
| ☐   | 18   | SKP-16 | Fitness suite v0                                                | M0/S3      | 5                          | [P]  |
| ☐   | 19   | SKP-17 | SKP e2e demo + archive (M0 exit)                                | M0/S3      | 16,13,14,15,18             | None |
| ☐   | 20   | FND-01 | Migrations framework + core schemas                             | M1/F0      | 19                         | None |
| ☐   | 21   | FND-02 | Profiles, principals, organizations                             | M1/F0      | 20                         | None |
| ☐   | 22   | FND-03 | Policy compiler v1 (non-weakening)                              | M1/F0      | 21                         | None |
| ☐   | 23   | FND-04 | OPA PDP integration                                             | M1/F0      | 22                         | [P]  |
| ☐   | 24   | FND-05 | ApprovedPlan full chain                                         | M1/F1      | 21,8                       | None |
| ☐   | 25   | FND-06 | OIDC + WebAuthn approvals (C12)                                 | M1/F1      | 21                         | [P]  |
| ☐   | 26   | FND-07 | External-operation ledger + outbox (C9)                         | M1/F1      | 20                         | [P]  |
| ☐   | 27   | FND-08 | GitHub SCM adapter, kernel-only push (C4)                       | M1/F1      | 26                         | None |
| ☐   | 28   | FND-09 | Authority import-boundary fitness                               | M1/F1      | 27                         | [P]  |
| ☐   | 29   | FND-10 | Cost ledger v1 (C19)                                            | M1/F2      | 20                         | None |
| ☐   | 30   | FND-11 | Telegram engine v1 (C11)                                        | M1/F2      | 21                         | [P]  |
| ☐   | 31   | FND-12 | Observability baseline                                          | M1/F2      | 12                         | [P]  |
| ☐   | 32   | FND-13 | Liveness, retry, PROVEN_BLOCKED (C22)                           | M1/F2      | 12                         | None |
| ☐   | 33   | FND-14 | Control-plane basics                                            | M1/F2      | 31                         | [P]  |
| ☐   | 34   | FND-15 | Rootless OCI executor sandbox                                   | M1/F2      | 10                         | None |
| ☐   | 35   | FND-16 | Secrets interface + file backend                                | M1/F3      | 20                         | [P]  |
| ☐   | 36   | FND-17 | API server (CLI parity)                                         | M1/F3      | 21,14                      | [P]  |
| ☐   | 37   | FND-18 | Documentation lint in CI                                        | M1/F3      | 2                          | [P]  |
| ☐   | 38   | FND-19 | Projector v2: rebuild + lag alert (C3)                          | M1/F3      | 14,31                      | None |
| ☐   | 39   | FND-20 | Backup/restore drill v0 (M1 exit)                               | M1/F3      | 20                         | None |
| ☐   | 40   | VEN-01 | MissionContract engine (C18)                                    | A/A1       | 21,29                      | None |
| ☐   | 41   | VEN-02 | Mission Setup Ceremony (C17)                                    | A/A1       | 40                         | None |
| ☐   | 42   | VEN-03 | Requirement→spec synthesizer (C16)                              | A/A1       | 21                         | [P]  |
| ☐   | 43   | VEN-04 | Mockup ingestion v0 (C16)                                       | A/A1       | 42                         | [P]  |
| ☐   | 44   | VEN-05 | PLAN generator from spec                                        | A/A1       | 42                         | None |
| ☐   | 45   | VEN-06 | Classifier v1: detected effects (C6)                            | A/A1       | 7,27                       | None |
| ☐   | 46   | VEN-07 | Product template repository                                     | A/A2       | 1                          | [P]  |
| ☐   | 47   | VEN-08 | Personal deploy adapter + profile gate (C13)                    | A/A2       | 22,46                      | None |
| ☐   | 48   | VEN-09 | Synthetic verification suite (C21)                              | A/A2       | 46                         | [P]  |
| ☐   | 49   | VEN-10 | Stripe test-mode billing + reconciler (C19)                     | A/A2       | 29,46                      | None |
| ☐   | 50   | VEN-11 | Observation loop → mission evaluation                           | A/A3       | 40,49                      | None |
| ☐   | 51   | VEN-12 | Bounded autonomous improvement cycle                            | A/A3       | 45,47                      | None |
| ☐   | 52   | VEN-13 | Weekly veto digest v0 (C11/C20)                                 | A/A3       | 30,51                      | [P]  |
| ☐   | 53   | VEN-14 | Venture MLS e2e (Track A exit)                                  | A/A3       | 41,43,44,47,48,49,50,51,52 | None |
| ☐   | 54   | TX-01  | Organization profile + governance (C14)                         | B/B1       | 22                         | None |
| ☐   | 55   | TX-02  | Org plan provenance validation (C7,C12)                         | B/B1       | 24,25                      | None |
| ☐   | 56   | TX-03  | PEC v1: proposals + prohibitions (C5)                           | B/B1       | 6                          | [P]  |
| ☐   | 57   | TX-04  | Atomic group + change-set manifest                              | B/B2       | 6                          | None |
| ☐   | 58   | TX-05  | Branch Integrator: lease/fencing/receipts (C4)                  | B/B2       | 27,57                      | None |
| ☐   | 59   | TX-06  | Drift guard + requeue + PROVEN_BLOCKED                          | B/B2       | 58                         | [P]  |
| ☐   | 60   | TX-07  | Handoff terminal + notification (C15)                           | B/B3       | 58                         | None |
| ☐   | 61   | TX-08  | Prohibited-operations tests (C15)                               | B/B3       | 60                         | [P]  |
| ☐   | 62   | TX-09  | Bitbucket adapter (optional, B2)                                | B/B3       | 58                         | [P]  |
| ☐   | 63   | TX-10  | 10x MLS e2e + live dry-run (Track B exit)                       | B/B3       | 55,56,59,60,61             | None |
| ☐   | 64   | HRD-01 | Fault-injection suite                                           | M2         | 53 or 63                   | None |
| ☐   | 65   | HRD-02 | Backpressure + fairness complete                                | M2         | 33                         | [P]  |
| ☐   | 66   | HRD-03 | Retention/PII enforcement (UU PDP)                              | M2         | 20                         | [P]  |
| ☐   | 67   | HRD-04 | Audit hash-chain verify + tamper drill                          | M2         | 20                         | [P]  |
| ☐   | 68   | HRD-05 | SLO alerts + runbooks (full catalog)                            | M2         | 31                         | [P]  |
| ☐   | 69   | HRD-06 | Cost reconciliation + cap proofs (C19)                          | M2         | 29,49                      | None |
| ☐   | 70   | HRD-07 | Security review + injection red-team                            | M2         | 34,64                      | None |
| ☐   | 71   | HRD-08 | DR drill automation                                             | M2         | 39                         | [P]  |
| ☐   | 72   | HRD-09 | Telegram hardening: fuzz + flood soak                           | M2         | 30                         | [P]  |
| ☐   | 73   | HRD-10 | Versioned release + upgrade path (M2 exit)                      | M2         | 64,65,66,67,68,69,70,71,72 | None |
| ☐   | 74   | EVO-01 | L0 auto-promotion pipeline                                      | M3         | 73                         | None |
| ☐   | 75   | EVO-02 | CumulativeChangeBudget + freeze + digest (C20)                  | M3         | 74                         | None |
| ☐   | 76   | EVO-03 | Memory curator with provenance                                  | M3         | 66                         | [P]  |
| ☐   | 77   | EVO-04 | Capability evolution loop (bounded L1)                          | M3         | 75                         | None |
| ☐   | 78   | EVO-05 | Multi-repository 10x saga                                       | M3         | 63                         | [P]  |
| ☐   | 79   | EVO-06 | OpenAI + local-model providers                                  | M3         | 34                         | [P]  |
| ☐   | 80   | EVO-07 | Figma API mockup ingestion                                      | M3         | 43                         | [P]  |
| ☐   | 81   | EVO-08 | Portfolio scaling: multi-mission                                | M3         | 53                         | None |
| ☐   | 82   | EVO-09 | Capacity-aware learning                                         | M3         | 74                         | [P]  |
| ☐   | 83   | EVO-10 | BillingMaturity graduation → bounded A2 (C19)                   | M3         | 69                         | None |

### D-P1 — Milestone dependencies

```mermaid
flowchart TD
    M0[M0 Tasks 1-19<br/>Shared Kernel Proof] --> M1[M1 Tasks 20-39<br/>Production Foundation]
    M1 --> A[Track A Tasks 40-53<br/>Venture MLS]
    M1 --> B[Track B Tasks 54-63<br/>10x MLS]
    A --> M2[M2 Tasks 64-73<br/>Hardening]
    B --> M2
    M2 --> M3[M3 Tasks 74-83<br/>Evolution]
    A -.parallel, independent gates.- B
```

### D-P2 — Parallel roadmap

```mermaid
flowchart LR
    subgraph Shared
        S[Tasks 1-19] --> F[Tasks 20-39]
    end
    subgraph TrackA[Track A]
        A1[40-45] --> A2[46-49] --> A3[50-53]
    end
    subgraph TrackB[Track B]
        B1[54-56] --> B2[57-59] --> B3[60-63]
    end
    F --> A1
    F --> B1
    A3 --> H[64-73] --> E[74-83]
    B3 --> H
```

---

## E. Milestone M0 — Shared Kernel Proof (Tasks 1–19)

**Objective:** CLI → admit one PLAN → one local repo → isolated worktree → one executor → deterministic validation → evidence bundle → **resume after forced process kill**. **Non-goals:** no Telegram, OPA, GitHub, deploys, billing, missions, PEC, multi-repo, UI. **Effort:** 2–4 wks solo+AI (High confidence). **Exit (Task 19):** `make skp-e2e` green incl. resume ×20; Constitution Check green; evidence archived. **Rollback:** tag `skp-w<N>` per wave; projections drop-recreate by design.

### D-P3 — SKP flow

```mermaid
flowchart LR
    CLI[foundry plan submit] --> APP[foundry plan approve<br/>Ed25519 over digest]
    APP --> ADM[Admission v0<br/>tier + self-classification reject]
    ADM --> WT[Isolated worktree]
    WT --> EX[Executor fake or claude-code]
    EX --> VAL[Deterministic validation]
    VAL --> EV[Evidence bundle]
    EV --> TERM[SUCCEEDED or FAILED + result_code]
    KILL[kill -9 worker] -.checkpoint + replay.-> EX
```

---

### Task 1 (HAR-01) — Repository scaffold, Docker-wrapped Makefile, CI

- **Goal:** Create the `delivery-foundry` repo with the authoritative layout, a **Docker-wrapped** Make targets contract, and CI so every later task has deterministic, host-independent entry points.
- **Rationale:** Agents need stable commands; humans need one bootstrap path; and the host should need only Docker + GNU make — no local Go/Node/Playwright toolchain — so the plan reproduces identically on any machine, any CI runner, and any other agent's sandbox.
- **Depends:** — · **Governing docs:** `docs/architecture/overview.md` (repo layout), §C conventions (Docker execution model).
- **Scope:** Directory skeleton with `doc.go` placeholders; `deploy/Dockerfile.dev` (the toolchain image); `deploy/docker-compose.yaml` skeleton (one `dev` service — Task 4 appends `postgres`/`temporal`); Makefile wrapping every target in `docker compose run`; CI using that same image; go.mod; linters config. No business logic.
- **Out of scope:** Any `internal/*` implementation; the `postgres`/`temporal` runtime services (Task 4 adds them to the compose file created here); the agent harness (Task 2).
- **Steps:** (1) `git init delivery-foundry`, `go mod init github.com/<owner>/delivery-foundry`. (2) Create tree: `cmd/foundry`, `cmd/foundryd`, `internal/{state,plan,admission,provenance,policy,kernel,pec,worktree,scm,executor,verify,evidence,ledger,projection,notify,mission,spec,recovery,observe}`, `migrations/`, `deploy/`, `examples/plans/`, `evidence/.gitkeep`, `test/`. Each internal pkg gets `doc.go` with one-line authority statement. (3) `deploy/Dockerfile.dev`: multi-stage, base `golang:1.22`, installs `make git nodejs npm docker-cli goose golangci-lint goreleaser` plus Playwright system deps (needed by Tasks 46/48); non-root user `foundry`, UID passed as build-arg so bind-mounted files keep sane host ownership. (4) `deploy/docker-compose.yaml` skeleton: service `dev` — `build: {context: ., dockerfile: deploy/Dockerfile.dev}`, `volumes: [".:/workspace", "gomod-cache:/go/pkg/mod", "gobuild-cache:/root/.cache/go-build"]`, `working_dir: /workspace`; named volumes declared at file scope. (5) `Makefile` implementing the §C target contract as thin wrappers: `COMPOSE := docker compose -f deploy/docker-compose.yaml` and `RUN := $(COMPOSE) run --rm dev`; e.g. `bootstrap: $(COMPOSE) build dev && $(RUN) go mod download`; `test: $(RUN) go test ./...`; `lint: $(RUN) golangci-lint run`; `fitness: $(RUN) bash scripts/fitness.sh`; unimplemented targets `echo "not yet: <target>" && exit 1` EXCEPT `bootstrap|test|lint|fitness` which must pass now. (6) `.dockerignore` (`.git`, `evidence/`, build artifacts — keep `docs/` since Task 2 needs it in-image). (7) `.github/workflows/ci.yaml`: **no `actions/setup-go`** — instead `docker compose -f deploy/docker-compose.yaml build dev` then `make bootstrap test lint fitness` on push/PR, so CI runs the exact same image as local and agent runs. (8) `golangci-lint` config (govet, errcheck, staticcheck, gofumpt) — lives in the image, invoked via `make lint`. (9) `README.md`: "Requirements: Docker + GNU make. Nothing else." + pointer to `docs/PLAN.md` §A protocol.
- **Outputs:** the tree above; `Makefile`; `deploy/Dockerfile.dev`; `deploy/docker-compose.yaml` (skeleton); `.dockerignore`; `.github/workflows/ci.yaml`; `.golangci.yml`; `scripts/fitness.sh`; `README.md`.
- **Acceptance:** on a clean machine with **only Docker and make installed (no Go toolchain on host)** — fresh clone → `make bootstrap && make test && make lint && make fitness` all exit 0; CI green on first push using the identical image; every internal package compiles with `doc.go` only; exactly one `dev` image is built.
- **Validation:** `make bootstrap test lint fitness` run from a Go-less shell/VM + CI run URL in evidence.
- **Evidence:** CI log, `tree -L 2` output, transcript proving host independence (Go-less shell running `make test` successfully). · **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** no logic beyond linters and the toolchain image itself.
- **Status:** ☐ Not started

### Task 2 (HAR-02) [P] — Agent harness: AGENTS.md, .ai skills/prompts, vendored docs

- **Goal:** Make the repo self-describing for AI agents: everything `implement-and-review-task.md` references must exist.
- **Rationale:** The execution prompt reads `AGENTS.md`, `docs/architecture.md`, `docs/PLAN.md`, `.ai/skills/task-implementation/SKILL.md`, `.ai/skills/task-review/SKILL.md`, `.ai/prompts/pr-remediation.md`. Missing harness = broken loop from day one.
- **Depends:** 1 · **Governing docs:** this plan §A–§C; `docs/security/reviewer-independence.md`; `docs/governance/quality-rubric.md`.
- **Scope:** Harness files + vendoring the V12 doc set + placing this plan.
- **Out of scope:** Modifying any V12 normative content; implementing tooling.
- **Steps:** (1) Copy the V12 set into repo: root `delivery_foundry.md` → `docs/delivery_foundry.md`, its `docs/**` → `docs/**` (preserve relative links: adjust the root file's `docs/...` links to `./...`? **No** — instead vendor as-is under `docs/foundry/` keeping internal structure: `docs/foundry/delivery_foundry.md` + `docs/foundry/docs/**`; links stay valid). (2) Write `docs/architecture.md`: 1-page orientation = constitution table copy + link map into `docs/foundry/...` (state model, authority, admission, provenance, tracks). (3) Place this file at `docs/PLAN.md`. (4) `AGENTS.md` at repo root: build/test commands (§C make contract), code style, authority rules ("only `internal/kernel` performs side effects; `internal/pec` proposes only"), task protocol summary (§A), forbidden actions (no force-push, no scope beyond task card, no future tasks). (5) `.ai/skills/task-implementation/SKILL.md`: read card → restate scope+out-of-scope → implement Steps in order → write tests alongside → run Validation → self-check against Acceptance list. (6) `.ai/skills/task-review/SKILL.md`: checklist = PLAN compliance, constitution articles named in card, architecture (authority boundaries), tests (failure paths present), security (secrets, injection, path traversal), complexity (no speculative abstraction), release readiness (migrations reversible, flags default-off). (7) `.ai/prompts/pr-remediation.md`: format `[<severity BLOCKER|MAJOR|MINOR>] <file:line> — <finding> → <exact fix>`; no prose filler.
- **Outputs:** `AGENTS.md`; `docs/architecture.md`; `docs/PLAN.md` (this file); `docs/foundry/**` (vendored V12); `.ai/skills/task-implementation/SKILL.md`; `.ai/skills/task-review/SKILL.md`; `.ai/prompts/pr-remediation.md`.
- **Acceptance:** every path referenced by `implement-and-review-task.md` exists; `docs/foundry` internal links resolve (`scripts/fitness.sh` link check extended to cover it); AGENTS.md ≤ 150 lines.
- **Validation:** `make fitness` (link check) + `test -f` script over the seven paths.
- **Evidence:** file list + link-check output. · **Risk:** Low · **Exec:** integration · **Rev:** R1 · **Boundary:** no V12 content edits.
- **Status:** ☐ Not started

### Task 3 (RUN-01) [P] — Autonomous PLAN runner: risk-tiered orchestrator with Telegram gate

- **Goal:** Close the loop this whole plan depends on — a standing process that reads `docs/PLAN.md`'s Master Index, picks the next eligible task, drives its implementation, and reports to _itself_ rather than to a human. Human involvement is minimized to a Telegram gate, for exactly the tasks this plan already marks as deserving one.
- **Rationale:** Without this, "autonomous loops" (§A) is a name with no implementation, and the plan's own thesis — minimal human touch — doesn't apply to building itself. This is a deliberately **temporary bootstrap tool**, not a second kernel: it never gains authority beyond what each task card already grants it (Risk/Rev fields, written by the plan itself), and it is retired the moment Foundry can run its own backlog directly (see Exit condition below). It must not become the thing the constitution's C4/C5 exist to prevent — a second orchestrator with its own authority.
- **Depends:** 2 (needs `AGENTS.md` and the `.ai/skills/*` files to know the implementation protocol; transitively needs Task 1). **Note:** Tasks 1, 2, and this task are necessarily human-triggered — nothing can orchestrate before the orchestrator exists. From Task 4 onward, this runner is the default trigger (§A).
- **Governing docs:** this plan §A (protocol), §B/§C (constitution and conventions the runner enforces verbatim, not a reinterpretation of them); `docs/foundry/docs/autonomy/admission-tiers.md` (the A0/A1/A2/H tiering pattern this reuses, mapped onto each card's own `Risk`/`Rev` fields instead of a new classifier).
- **Scope:** a standalone tool living outside `internal/` — it is bootstrap tooling for _building_ Foundry, not part of the shipped Foundry binary, the same way the Makefile itself is tooling and not a product package.
- **Out of scope:** modifying any `internal/*` package directly (it only _invokes_ the same implementation protocol a human would); acting as a permanent second orchestrator (see Exit condition); inventing its own risk classifier (it reads the tier straight off the card).
- **Steps:** (1) Parser: read `docs/PLAN.md`, extract the Master Index (checkbox, task number, alias, title, depends, `[P]`) and, for a candidate task, its full card body via `### Task N` section boundaries. (2) Eligibility: a task is eligible once every number in its `Depends` shows ✅ in the Index; `[P]`-marked eligible tasks with disjoint declared Outputs may dispatch concurrently, capped at 2 by default. (3) Classification — reuse, don't reinvent: read the card's own `Risk:` and `Rev:` fields. `Risk ∈ {Low, Med}` **and** `Rev ∈ {R1, R2}` → **AUTO**. `Risk = High` **or** `Rev ∈ {R3, R4}` → **GATED**. This is exactly the A0/A1/A2 vs. H split from `admission-tiers.md`, applied here with zero new classification logic. (4) **AUTO path:** invoke the implementation protocol headlessly (e.g. `claude -p`, non-interactive, prompt = card content + `AGENTS.md` + `.ai/skills/task-implementation/SKILL.md`, run inside `dev` per §C) → capture its report → run the card's own Validation commands plus repo-wide `make test fitness` → on green: commit on `task/<N>-<slug>`, merge, flip `Status: ✅ <date>`, check the Index box, append to §T → advance to the next eligible task. On failure: retry once; **two consecutive validation failures on the same task halts the entire runner** and sends a blocking Telegram alert — mirrors the recovery loop's no-progress rule (C22); it never keeps trying silently. (5) **GATED path:** implement, self-review, and validate identically, but stop _before_ commit; send a Telegram message naming the task, changed files, validation results, and the exact `Risk`/`Rev` reason it's gated; wait for a nonce-bound `/approve <id>` or `/reject <id>`. No reply within the configured window (default 24h) ⇒ stays paused; **never auto-approves.** (Using Telegram here is consistent with C11: this gates the runner's own commit to the _build_ repo, not a high-risk _product_ action — it is not a substitute for the strong-auth approval C12 requires once real ApprovedPlan provenance exists.) (6) **Reporting:** AUTO completions get a non-blocking batched Telegram digest — every 5 completions or 2 hours, whichever first — the same shape as the venture loop's weekly veto digest; it never blocks progress. (7) **Safety caps:** the digest batching size is also a drift cap (default 5 auto-completions between checkpoints); a Telegram `/freeze` command halts immediately, mirroring the later kill-switch pattern (Task 72/HRD-09). (8) **Exit condition — retire this tool:** once Foundry's own kernel (Task 12), deterministic classifier (Task 7), and Telegram engine (Task 30) exist, stop using this runner for new tasks. Instead, admit the remaining `PLAN.md` backlog as real `ApprovedPlan` documents executed by Foundry itself — the dogfooding this plan's own thesis points at. This tool's job is done at that point; it does not continue running alongside the real kernel.
- **Outputs:** `tools/planrunner/{main.go,parser.go,classify.go,dispatch.go,telegram.go}`; new Make target `plan-run` (docker-wrapped per §C, like every other target); `.env` entry for a disposable bootstrap Telegram bot token (explicitly documented as throwaway, distinct from Foundry's eventual production bot); a README section "running the plan autonomously," including the Exit condition above.
- **Acceptance:** against a scratch copy of `docs/PLAN.md` with two fixture tasks (one Low/R1, one High/R3), the runner auto-completes the low-risk fixture end-to-end with zero human input beyond the digest notification, and correctly pauses the high-risk fixture until a Telegram `/approve` is sent; two seeded consecutive validation failures halt the runner with an alert, not a silent retry loop.
- **Validation:** `go test ./tools/planrunner/...` plus a scripted dry run against the fixture plan (`test/planrunner_dryrun.sh`).
- **Evidence:** dry-run transcript showing one AUTO completion, one GATED pause+approve, and one halted-on-failure case.
- **Risk:** High (it has real, if bounded, authority — auto-commits code) · **Exec:** infra+go-kernel · **Rev:** **R3** (this task grants autonomy to everything after it, so it earns the strict review level itself) · **Boundary:** no authority beyond what each downstream card already declares in its own `Risk`/`Rev` fields.
- **Status:** ☐ Not started

### Task 4 (SKP-02) [P] — Runtime services in compose: Temporal + PostgreSQL, make up/doctor

- **Goal:** Add the runtime dependency services to the compose file Task 1 created: one command brings up both the toolchain (`dev`) and its dependencies (`postgres`, `temporal`) on a shared network.
- **Depends:** 1 · **Governing docs:** `docs/architecture/data-consistency.md` (two stores, one authority each); §C Docker execution model.
- **Scope:** append to the existing `deploy/docker-compose.yaml`: `postgres:16` (volume + healthcheck) and `temporalio/auto-setup` dev server + UI port 8233, both reachable from `dev` by service name (`postgres:5432`, `temporal:7233`) — no host port juggling needed for internal calls; `make up down doctor`; `.env.example` (`PG_DSN=postgres://foundry:foundry@postgres:5432/foundry`, `TEMPORAL_HOSTPORT=temporal:7233`, `FOUNDRY_DATA_DIR=/workspace/.data`).
- **Out of scope:** production deployment; OPA; object stores; the `dev` toolchain image itself (Task 1 owns it).
- **Steps:** (1) add `postgres`+`temporal` services and healthchecks to the compose file from Task 1. (2) `up: $(COMPOSE) up -d postgres temporal`. (3) `doctor: $(RUN) go run ./cmd/foundry doctor` — runs _inside_ `dev`, pings PG (`SELECT 1`) and Temporal (`GetSystemInfo`) over the compose network, prints PASS/FAIL, exits 1 on any FAIL; **additionally** the bare `make doctor` invocation first checks `docker version`/`docker compose version` on the host and prints an actionable install link if either is missing, before attempting the containerized checks. (4) document ports in README (8233 UI published to host for convenience; 5432/7233 internal-network only since only `dev` needs them).
- **Outputs:** `deploy/docker-compose.yaml` (postgres+temporal services added to Task 1's file); `cmd/foundry/doctor.go`; `.env.example`; Make targets wired.
- **Acceptance:** `make up && make doctor` exits 0 within 60s on a clean Docker-only machine; `make down` leaves no volumes unless `KEEP_DATA=1`; on a machine with Docker absent, `make doctor` fails fast with the actionable message (not a raw connection-refused error).
- **Validation:** `make up doctor down` in CI + a Docker-absent negative-test leg.
- **Evidence:** doctor output for both the positive and the Docker-absent case. · **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** no schema creation (Task 14/19 own migrations); no changes to the `dev` image itself.
- **Status:** ☐ Not started

### Task 5 (SKP-03) [P] — Canonical state package (C1)

- **Goal:** The single source of workflow lifecycle truth every package imports.
- **Rationale:** C1 lives or dies here; drift here poisons everything downstream.
- **Depends:** 1 · **Governing docs:** `docs/foundry/docs/architecture/state-model.md` (§1 lifecycle, §2 registries, §3 historical mapping, §4 fitness rules).
- **Scope:** `internal/state` only. **Zero imports** beyond stdlib (fitness-tested Task 18).
- **Out of scope:** persistence, Temporal, JSON API shapes beyond the transition record.
- **Steps:** (1) Types: `type Status string` with the six constants; `Phase`, `Reason`, `ResultCode` string types with registry maps exactly matching state-model §2 (phases: intake, context-gathering, specification, planning, admission, implementation, verifying, reviewing, integrating, deploying, observing, improving, curating; reasons incl. subscription-reset, unforeseen-human-gate; result codes incl. PROVEN*BLOCKED, ADMISSION_REJECTED, ROLLED_BACK, TEN_X_BRANCH_HANDOFF_READY, MISSION\*\*). (2) `const DeprecatedAliasTenXBranchesReady = "TEN_X_BRANCHES_READY"` mapping helper `NormalizeResultCode(string) (ResultCode, bool)` — alias accepted on read, never emitted. (3) Transition record struct: `{WorkflowID string; Status Status; PhaseFrom, PhaseTo Phase; Reason Reason; ResultCode ResultCode; Actor, Profile string; Evidence []string; CheckpointID string; Attempt int; NextAction string; WakeAt *time.Time; OccurredAt time.Time}`+`Validate(from, to Status) error` implementing PENDING→RUNNING; RUNNING⇄WAITING; RUNNING→{SUCCEEDED,FAILED,CANCELLED}; WAITING→{RUNNING,CANCELLED,FAILED}; terminals absorb. (4) Invariants in code: WAITING requires Reason; FAILED with result requires registry code; SUCCEEDED forbids Reason. (5) Table-driven tests for every legal/illegal edge + alias normalization + registry completeness against a golden list.
- **Outputs:** `internal/state/{status.go,registries.go,transition.go,alias.go}` + `_test.go` files.
- **Acceptance:** illegal transitions error; alias normalizes and is never produced by `String()`; registries match state-model doc verbatim (golden test reads the doc's code block via `docs/foundry/...` path and diffs).
- **Validation:** `go test ./internal/state/... -count=1 -race` ; `grep -R "TEN_X_BRANCHES_READY" internal/ --include='*.go' | grep -v alias.go` returns empty.
- **Evidence:** test output + golden diff proof. · **Risk:** Med (foundation) · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** no persistence, no serialization framework.
- **Status:** ☐ Not started

### Task 6 (SKP-04) [P] — PLAN schema, parser, canonical digest

- **Goal:** Parse executable PLAN.md documents into typed structures with a stable content digest.
- **Depends:** 1 · **Governing docs:** `docs/foundry/docs/workflows/direct-plan.md` (PLAN contract), `docs/foundry/docs/security/approval-and-provenance.md` (digest binding).
- **Scope:** `internal/plan`; three example plans; golden tests.
- **Out of scope:** admission logic; generation (Task 44).
- **Steps:** (1) Schema structs: `Document{ID, Title, Version string; Repos []RepoRef; Tasks []Task; DeclaredEffects []Effect; RequestedPermissions []Permission; DeclaredTierIgnored string; BudgetUSD float64}`; `Task{ID, Goal string; DependsOn []string; Commands []string; ValidationCommands []string; Files []string}`; `Effect{Kind, Target string}` with Kind enum (docs, code, dependency, migration, billing, secret, network, deploy, permission, destructive). (2) Markdown front-matter (YAML) + sectioned body parser; unknown fields = error (strict). (3) Canonical digest: normalize CRLF→LF, trim trailing spaces, UTF-8 NFC, then sha256; expose `Digest() [32]byte` + hex. (4) `DeclaredTierIgnored`: parse if present, set `SelfClassified=true` flag — consumed by Task 7 to reject. (5) Examples: `examples/plans/hello-world.md` (one task, echo+test), `two-task.md` (dependency chain), `failing-task.md` (validation command exits 1). (6) Golden tests: parse→reserialize→reparse digest stability; fuzz parser (go fuzz) 30s corpus.
- **Outputs:** `internal/plan/{schema.go,parse.go,digest.go,effects.go}` + tests + `examples/plans/*.md`.
- **Acceptance:** digest stable across whitespace/line-ending permutations; strict-mode rejects unknown keys with line numbers; SelfClassified flag set when tier declared.
- **Validation:** `go test ./internal/plan/... -race`; `go test -fuzz=FuzzParse -fuzztime=30s ./internal/plan/`.
- **Evidence:** golden corpus committed. · **Risk:** Low · **Exec:** go-backend · **Rev:** R1 · **Boundary:** no LLM calls, no file writes outside testdata.
- **Status:** ☐ Not started

### Task 7 (SKP-05) — Deterministic AdmissionClassifier v0 (C6)

- **Goal:** Versioned, pure-function tier computation over declared effects; self-classifying plans fail closed.
- **Depends:** 6 · **Governing docs:** `docs/foundry/docs/autonomy/admission-tiers.md` (§1 classifier, §2 tiers, D-31).
- **Scope:** `internal/admission`; declared effects only (detected effects = Task 45).
- **Out of scope:** LLM effect extraction; policy store integration (uses injected `PolicyView` interface stub).
- **Steps:** (1) `Decision{ClassifierVersion, PolicyDigest string; RulesEvaluated []string; Declared, Detected, Discrepancies []plan.Effect; RiskScore float64; Tier Tier; RequiredControls []string; Explanation string}`; `Tier` enum A0 A1 A2 H. (2) Ruleset v1 as ordered data (slice of `{ID, Match func, TierFloor}`): docs/copy/tests→A0 floor; dependency|migration|network|secret|deploy→A1 floor; billing|permission|destructive→H; production deploy→A2 floor (profile-gated later). Highest floor wins; every fired rule ID appended. (3) Hard gate first: `if doc.SelfClassified → return Decision{Tier: H, Explanation: "plan-authored tier ignored"}, ErrSelfClassification` → caller maps to `FAILED/result_code: ADMISSION_REJECTED`. (4) Determinism: no maps iterated for output ordering; sort everything; version string `admission/v1.0`. (5) Golden corpus: ≥12 plans covering every rule + combinations; run each 5× asserting byte-identical Decision JSON.
- **Outputs:** `internal/admission/{tier.go,classifier.go,rules_v1.go,decision.go}` + `testdata/golden/*.json` + tests.
- **Acceptance:** identical input ⇒ identical marshaled Decision (×5); self-classified fixture rejected; discrepancy slot present (empty in v0) so Task 45 extends without breaking schema.
- **Validation:** `go test ./internal/admission/... -run Golden -count=5 -race`.
- **Evidence:** golden JSONs. · **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** pure functions only; no I/O, no clock, no rand.
- **Status:** ☐ Not started

### Task 8 (SKP-06) — Signed ApprovedPlan provenance v0 (C7)

- **Goal:** Ed25519-signed approval bound to the plan digest; kernel accepts only verified ApprovedPlans.
- **Depends:** 6, 4 · **Governing docs:** `docs/foundry/docs/security/approval-and-provenance.md` (§1 artifacts, §2 rules).
- **Scope:** `internal/provenance`; CLI `foundry keygen | plan submit | plan approve | plan verify`; PG table write (raw SQL, migrations formalized Task 20 — create `approved_plans` here via `migrations/0001_approved_plans.sql` with down).
- **Out of scope:** OIDC/WebAuthn (Task 25); expiry/revocation flows (Task 24 — columns exist, unenforced).
- **Steps:** (1) Artifacts: `PlanSubmission{Digest, Source, Submitter, At}` → `AdmissionDecision` (from Task 7) → `ApprovedPlan{PlanID, PlanDigest, CreatorPrincipal, SubmittingPrincipal, ClassifierVersion, Declared, Requested, Granted, Scope, RiskTier, BudgetEnvelope, DataClass, Approvers[], AuthMethod:"ed25519-local", ApprovedAt, ExpiresAt, Revoked bool, Signature}`. Granted = Requested ∩ policy stub allowlist (config file `config/permissions-allowlist.yaml`). (2) Signing payload = canonical JSON of all fields minus Signature; `foundry keygen` writes `~/.foundry/keys/approver.{pub,key}` 0600. (3) `plan approve <file>` runs Task 7 classify → on A-tier or explicit `--force-h-ack` prints decision, signs, INSERTs. (4) `plan verify <plan-id>` recomputes digest from file + verifies signature + prints granted⊆requested proof. (5) Kernel-facing API: `Load(ctx, planID) (*ApprovedPlan, error)` verifying signature on every load (tamper = error). (6) Tests: byte-flip tampering (file and DB row), unsigned insert rejected, granted⊄requested impossible by construction.
- **Outputs:** `internal/provenance/{artifacts.go,sign.go,store.go,verify.go}`; `cmd/foundry/{keygen.go,plan_submit.go,plan_approve.go,plan_verify.go}`; `migrations/0001_approved_plans.sql`; `config/permissions-allowlist.yaml`; tests incl. e2e script `test/provenance_e2e.sh`.
- **Acceptance:** tampered plan byte ⇒ verify fails and kernel Load errors; approving runs classification (decision persisted alongside); CLI round-trip green in e2e script.
- **Validation:** `go test ./internal/provenance/... -race && bash test/provenance_e2e.sh`.
- **Evidence:** e2e transcript. · **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** local keys only; no network identity.
- **Status:** ☐ Not started

### Task 9 (SKP-07) [P] — Worktree manager (C8)

- **Goal:** Per-task isolated git worktrees; canonical clone is read-only to agents.
- **Depends:** 1 · **Governing docs:** `docs/foundry/docs/workflows/multi-repository.md` (workspace model N10).
- **Scope:** `internal/worktree`; local repos only.
- **Steps:** (1) `Manager{Root string}` with `Acquire(ctx, repoPath, wfID, taskID string) (Workspace, error)` → `git worktree add <root>/<wf>/<task> <base-branch>` on a detached branch `foundry/<wf>/<task>`; `Workspace{Path, Branch, Release func}`. (2) Locking: flock per repo during add/remove; concurrent acquires race-tested. (3) `Release` = `git worktree remove --force` + branch delete when terminal; orphan sweep `SweepOlderThan(d)`. (4) Canonical-protection test: after N parallel acquires+writes, canonical `git status --porcelain` empty and `git fsck` clean.
- **Outputs:** `internal/worktree/{manager.go,lock.go}` + tests with fixture repo builder `test/fixtures/repo.go`.
- **Acceptance:** 10 concurrent tasks, zero path collisions (`-race -count=10`); canonical untouched proof; sweep removes only orphans.
- **Validation:** `go test ./internal/worktree/... -race -count=10`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Boundary:** no pushes, no remotes.
- **Status:** ☐ Not started

### Task 10 (SKP-08) [P] — Executor contract + fake executor

- **Goal:** The adapter seam all executors implement, plus a deterministic fake for every test.
- **Depends:** 1 · **Governing docs:** `docs/foundry/docs/providers/provider-execution-classes.md` (adapter contract).
- **Scope:** `internal/executor` + `internal/executor/fake`.
- **Steps:** (1) `type Adapter interface { Prepare(ctx, ws worktree.Workspace, packet TaskPacket) error; Run(ctx) (Summary, error); Collect(ctx) (Artifacts, error) }`; `TaskPacket{PlanID, TaskID, Goal string; Commands, ValidationCommands []string; EnvAllowlist []string; TimeoutSec int}`; `Summary{Claimed string; ExitNotes string}` (explicitly _untrusted_ — doc.go says so); `Artifacts{Paths []string}`. (2) Subprocess harness: scrubbed env (only allowlist), working dir = workspace, `syscall.SysProcAttr{Setpgid}` + kill process group on timeout/cancel. (3) Fake executor: reads script `fake_script.yaml` from packet-referenced testdata — applies file patches, sleeps, exits, and can emit a **lying Summary** ("all tests pass") for honest-completion tests. (4) Registry: `executor.Get(name)`.
- **Outputs:** `internal/executor/{adapter.go,subprocess.go,registry.go}`, `internal/executor/fake/{fake.go,script.go}` + tests + `test/fixtures/fake_scripts/*.yaml` (success, fail, lie, timeout).
- **Acceptance:** timeout kills entire process tree (orphan-check test); env leak test proves only allowlisted vars visible; lying script produces Summary contradicted later by Task 13.
- **Validation:** `go test ./internal/executor/... -race`.
- **Risk:** Low · **Exec:** go-backend · **Rev:** R1 · **Boundary:** no network in fake; no real LLM adapters (Task 17).
- **Status:** ☐ Not started

### Task 11 (SKP-09) [P] — Evidence bundle + FS object store (C10)

- **Goal:** Tamper-evident, offline-verifiable proof of what actually ran.
- **Depends:** 1 · **Governing docs:** `docs/foundry/docs/governance/quality-rubric.md` (evidence), `docs/foundry/docs/operations/observability-and-alerts.md` §2 (artifacts live outside workflow history).
- **Scope:** `internal/evidence`; filesystem store under `$FOUNDRY_DATA_DIR/evidence`.
- **Steps:** (1) `Bundle{Manifest, Dir}`; `Manifest{WorkflowID, TaskID string; Commands []CommandRecord{Cmd, ExitCode, StdoutDigest, DurationMS}; Artifacts []ArtifactRef{Path, SHA256, Bytes}; Transitions []state.Transition; CreatedAt}`; manifest digest = sha256 of canonical JSON. (2) `Store{Put(Bundle) (ID, error); Get(ID); Verify(ID) error}` — Verify re-hashes every artifact + manifest. (3) CLI `foundry evidence verify <id>` and `foundry evidence show <id>`. (4) Content-addressed layout `evidence/<sha[0:2]>/<sha>/{manifest.json,artifacts/...}`.
- **Outputs:** `internal/evidence/{bundle.go,store_fs.go}`; `cmd/foundry/evidence.go`; tests incl. bit-flip detection.
- **Acceptance:** flip one artifact byte ⇒ Verify fails naming the file; bundles are immutable (second Put with same ID errors).
- **Validation:** `go test ./internal/evidence/... && foundry evidence verify` in e2e later.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Boundary:** FS only; S3 interface comment, not implementation.
- **Status:** ☐ Not started

### Task 12 (SKP-10) — Kernel workflow on Temporal (C2, C4)

- **Goal:** The `DeliverPlan` durable workflow: the only place sequencing, retries, and side effects live.
- **Depends:** 5,7,8,9,10,11 · **Governing docs:** `docs/foundry/docs/architecture/authority-model.md`, `state-model.md`, `data-consistency.md`.
- **Scope:** `internal/kernel` + `cmd/foundryd` worker. Single repo, sequential tasks (waves later via PEC).
- **Out of scope:** SCM pushes, network, projections (Task 14), PEC.
- **Steps:** (1) Workflow `DeliverPlan(planID)`: activity `LoadApprovedPlan` (signature-verified) → for each plan task: activities `AcquireWorktree` → `ExecuteTask` (adapter; heartbeats every 10s; StartToClose from packet timeout) → `ValidateTask` (Task 13) → `RecordEvidence` → emit transition. Terminal mapping: all validated ⇒ SUCCEEDED; validation fail ⇒ FAILED classification `verification-failed`; ctx cancel ⇒ CANCELLED. (2) Transitions emitted via activity `AppendTransition` (source of projection stream; store in PG table `workflow_transitions(workflow_id, seq bigserial, payload jsonb)` — created in `migrations/0002_transitions.sql`). (3) Side-effect discipline: only activities touch the world; workflow code deterministic (no time.Now/rand — use workflow.Now, SideEffect). (4) Leases/fencing: `AcquireLease(resource) (token)` activity + token checked in every mutating activity (worktree ops) — table `leases(resource pk, token, holder, expires_at)` in `0002`. (5) Idempotency: every activity takes an idempotency key `(wfID, taskID, activity, attempt-scope)`; re-execution returns recorded receipt (receipts table in `0002`). (6) Retry policies: activity-level (max 3, backoff 2×) for retryable classes only; deterministic failures do not retry. (7) Replay tests with `worker.WorkflowReplayer` over recorded histories (`test/histories/`). (8) Worker main: task queue `foundry-core`, graceful shutdown.
- **Outputs:** `internal/kernel/{workflow.go,activities.go,lease.go,idempotency.go,transitions.go}`; `cmd/foundryd/main.go`; `migrations/0002_transitions.sql`; replay tests + recorded histories.
- **Acceptance:** hello-world and failing-task example plans reach correct terminals; re-running a completed activity is a no-op via receipt; replay tests green; `go vet` + custom lint: no `time.Now` in workflow files.
- **Validation:** `go test ./internal/kernel/... -race` + `make up && go run ./test/e2e/skp_basic` (drives both plans end-to-end).
- **Evidence:** Temporal history export for both runs. · **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** no pushes; no projection reads inside workflow decisions.
- **Status:** ☐ Not started

### Task 13 (SKP-11) [P] — Deterministic validation runner (C10)

- **Goal:** Truth comes from commands, not executor claims.
- **Depends:** 10 · **Governing docs:** `docs/foundry/docs/workflows/recovery.md` (honest completion).
- **Scope:** `internal/verify`.
- **Steps:** (1) `Runner.Run(ctx, ws, cmds []string) ([]CommandRecord, error)`: each command exec'd argv-style (shlex split, **no shell**), cwd=workspace, env=minimal, output captured to evidence artifacts, 10-min default timeout each. (2) Allowlist: first token must be in `config/validation-allowlist.yaml` (go, make, npm, pnpm, pytest, bash-scripts under `./scripts/` only) — violation = deterministic failure `policy-violation`. (3) Honest-completion contract: kernel marks task result **solely** from Runner records; Summary stored but never trusted (test: lying fake ⇒ FAILED). (4) Classification: exit≠0 ⇒ `verification-failed`; allowlist breach ⇒ `policy-violation`; timeout ⇒ `retryable` once then `no-progress`.
- **Outputs:** `internal/verify/{runner.go,allowlist.go,classify.go}`; `config/validation-allowlist.yaml`; tests (incl. injection attempts: `; rm -rf`, backticks, env expansion — all inert).
- **Acceptance:** lying-summary fixture ends FAILED; injection corpus inert; records land in evidence bundle.
- **Validation:** `go test ./internal/verify/... -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Boundary:** no shell; no network.
- **Status:** ☐ Not started

### Task 14 (SKP-12) [P] — Status projection v0 (C3)

- **Goal:** Rebuildable PG read model of workflow status fed by the transition stream.
- **Depends:** 12 · **Governing docs:** `docs/foundry/docs/architecture/data-consistency.md` (§2 projection contract).
- **Steps:** (1) `migrations/0003_projection.sql`: `workflow_status_projection(workflow_id text pk, status text, phase text, reason text, result_code text, attempt int, checkpoint_id text, wake_at timestamptz, last_seq bigint not null, projector_version text not null, updated_at timestamptz)` + `projection_offsets(projector text pk, last_seq bigint)`. (2) Projector loop (in foundryd): poll `workflow_transitions` where seq > offset, idempotent upsert keyed (workflow_id) guarded `last_seq < new_seq`, advance offset transactionally. (3) `foundry projection rebuild`: truncate + replay from seq 0; assert row-count + digest match via `projection_checksum()` SQL function. (4) Metric `projection_lag_seconds` exposed (plain expvar now; OTel at Task 31).
- **Outputs:** `internal/projection/{projector.go,rebuild.go}`; migration 0003; `cmd/foundry/projection.go`; rebuild e2e test.
- **Acceptance:** drop table → rebuild → identical checksum; out-of-order/duplicate seq handled idempotently (property test).
- **Validation:** `go test ./internal/projection/... && bash test/projection_rebuild_e2e.sh`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Boundary:** projections never consulted by kernel decisions (doc.go + lint note).
- **Status:** ☐ Not started

### Task 15 (SKP-13) [P] — CLI status with consistency levels

- **Goal:** `foundry status <wf> [--fresh]` — projected read vs read-through to Temporal.
- **Depends:** 14 · **Governing docs:** data-consistency §2 (stale-read labeling).
- **Steps:** projected path reads PG and prints `consistency: projected (lag: Xs)`; `--fresh` calls Temporal DescribeWorkflowExecution + last transition query, prints `consistency: fresh`; induced-lag test (pause projector) shows divergence then convergence.
- **Outputs:** `cmd/foundry/status.go` + e2e test script.
- **Acceptance:** during induced lag, projected≠fresh detected by test; after resume, equal.
- **Validation:** `bash test/status_consistency_e2e.sh`.
- **Risk:** Low · **Exec:** go-backend · **Rev:** R1 · **Status:** ☐ Not started

### Task 16 (SKP-14) — Checkpoint + forced-restart resume proof (C22)

- **Goal:** The SKP thesis: kill -9 mid-plan, restart, complete without re-doing finished work.
- **Depends:** 12, 13 · **Governing docs:** `docs/foundry/docs/operations/disaster-recovery.md` (checkpoint/restart).
- **Steps:** (1) `internal/recovery/checkpoint.go`: checkpoint = last completed task ID + evidence IDs, stored as workflow state (Temporal history is the checkpoint; explicit CheckpointID recorded on transitions for operators). (2) e2e `test/skp_resume_test.sh`: start `two-task.md` via slow fake script; wait for task-1 evidence; `kill -9` foundryd; assert PG shows RUNNING (stale ok); restart worker; assert completion; assert task-1 executed exactly once via idempotency receipts count. (3) CI job runs it **20×** (`for i in $(seq 20)`), any failure = red. (4) Negative control: delete receipt ⇒ task re-runs (proves receipts are the guard).
- **Outputs:** `internal/recovery/checkpoint.go`; `test/skp_resume_test.sh`; CI wiring `make skp-resume`.
- **Acceptance:** 20/20 green in CI; receipts prove exactly-once effect.
- **Validation:** `make skp-resume`.
- **Evidence:** CI run + receipts query output. · **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 17 (SKP-15) [P] — Claude Code executor adapter (feature-flagged)

- **Goal:** One real executor behind `FOUNDRY_EXECUTOR=claude-code`.
- **Depends:** 10, 12 · **Governing docs:** `docs/foundry/docs/providers/anthropic.md` (staleness rule — verify CLI flags at implementation), Blocker B8.
- **Steps:** subprocess `claude` CLI in workspace jail (non-interactive/print mode with the task packet as prompt file; verify current flags/headless mode against official docs at implementation time); env allowlist excludes all secrets except executor auth; capture cost/token telemetry to Summary extras if emitted; timeout + kill group; integration test gated by `RUN_REAL_EXECUTOR=1` implementing hello-world on a fixture repo.
- **Outputs:** `internal/executor/claudecode/adapter.go` + gated integration test + `docs/notes/claude-code-flags.md` (verified flags snapshot, dated).
- **Acceptance:** gated test green locally; without flag, suite unaffected; no secret appears in workspace env dump.
- **Validation:** `RUN_REAL_EXECUTOR=1 go test ./internal/executor/claudecode/ -run Integration` (evidence: transcript).
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Boundary:** subscription-class capacity handling deferred to Task 31/M1 capacity work; log only.
- **Status:** ☐ Not started

### Task 18 (SKP-16) [P] — Fitness suite v0

- **Goal:** The constitution's teeth: violations fail CI.
- **Depends:** 5 · **Governing docs:** `docs/foundry/docs/architecture/state-model.md` §4; `docs/foundry/docs/governance/documentation-rules.md`.
- **Steps:** `scripts/fitness.sh` orchestrating: (a) enum lint — AST scan (`go/analysis` mini-tool `cmd/fitlint`) for any const block declaring ≥3 of the six status words outside `internal/state`; (b) superseded-term lint — repo grep for `TEN_X_BRANCHES_READY` outside `internal/state/alias.go`, state-model mapping, migration map, changelog; (c) import boundaries — `internal/state` imports stdlib only; `internal/scm` push symbols referenced only from `internal/kernel` (go list -deps check; activated fully at Task 28); (d) doc links — markdown link resolver over repo + `docs/foundry/**`; (e) seeded-violation self-test: fixtures under `test/fitness_seeds/` must each FAIL.
- **Outputs:** `cmd/fitlint/main.go`; `scripts/fitness.sh` (real now); `test/fitness_seeds/*`.
- **Acceptance:** all seeds fail; clean repo passes; runtime <60s.
- **Validation:** `make fitness` + seed harness `make fitness-selftest`.
- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Status:** ☐ Not started

### Task 19 (SKP-17) — SKP e2e demo + evidence archive (M0 exit)

- **Goal:** Prove M0 exit criteria and freeze the proof.
- **Depends:** 16,13,14,15,18 · **Governing docs:** this plan §E header.
- **Steps:** `make skp-e2e` = doctor → three plans (success, deterministic-fail, resume) → `foundry evidence verify` each → status consistency check → fitness → archive bundles + histories to `evidence/m0-exit/`; write `docs/notes/m0-exit-report.md` (dates, run IDs, 20× resume proof link); tag `v0.1.0-skp`.
- **Outputs:** `Makefile` target `skp-e2e`; `evidence/m0-exit/**`; `docs/notes/m0-exit-report.md`; git tag.
- **Acceptance:** single command green from clean `make up`; report lists every C-article touched in M0 with its proof.
- **Validation:** `make skp-e2e`.
- **Risk:** Low · **Exec:** integration · **Rev:** R2 · **Status:** ☐ Not started

**M0 tripwire:** if Task 16 is not green by end of week 4 under the solo assumption, stop and replan (sizing rule, `docs/foundry/docs/architecture/overview.md`).

---

## F. Milestone M1 — Shared Production Foundation (Tasks 20–39)

**Objective:** everything both tracks need: real profiles/policy, strong-auth provenance, GitHub under kernel authority, ledgers, Telegram, observability, recovery, sandbox. **Non-goals:** missions, billing, deploy adapters, PEC, 10x semantics, learning. **Effort:** 4–8 wks (Medium). **Exit (Task 39 + checklist):** real-repo e2e with kernel push; WebAuthn gate on High-risk approval; batching under flood; rebuild + audit verify green; brownout sheds correctly. **Rollback:** Temporal versioning/patching per change; flags per profile; reversible migrations.

### Task 20 (FND-01) — Migrations framework + core schemas

- **Goal:** Formal migration tooling (goose) owning all schema evolution; consolidate 0001–0003 and add core M1 schemas.
- **Depends:** 19 · **Governing docs:** `docs/foundry/docs/architecture/data-consistency.md` (PG authority list).
- **Scope:** `migrations/` + `make migrate-up migrate-down migrate-status`; schemas: `0004_principals.sql` (principals: id, kind human|service, display, idp_subject nullable, created_at; organizations; org_members(role)), `0005_profiles.sql` (profiles: id, name, kind personal|organization, org_id nullable, config jsonb, policy_digest, created_at), `0006_ledgers.sql` (external_operations: id, workflow_id, kind, target, idempotency_key unique, state reserved|executed|reconciled|failed, request jsonb, receipt jsonb, created_at, updated_at; cost_entries: id, scope workflow|product|mission, scope_id, state reserved|estimated|incurred|reconciled, amount_usd numeric(12,4), pricing_version, provider, meta jsonb, at), `0007_notifications.sql` (outbound queue + delivery state), `0008_audit.sql` (audit_log: seq bigserial, actor, action, subject, payload jsonb, prev_hash bytea, hash bytea — chain computed in trigger-free Go writer).
- **Out of scope:** business logic on these tables.
- **Steps:** adopt goose; port 0001–0003 into numbered goose files preserving data; add 0004–0008 with `down`; CI job runs up→down→up on scratch DB.
- **Outputs:** goose-managed `migrations/*.sql`; `internal/db/migrate.go`; CI wiring.
- **Acceptance:** up-down-up green in CI; every table commented (`COMMENT ON`) with authority note (projection vs authoritative).
- **Validation:** `make migrate-up migrate-down migrate-up && go test ./internal/db/...`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Boundary:** no ORM adoption; sqlc or hand SQL only. · **Status:** ☐ Not started

### Task 21 (FND-02) — Profiles, principals, organizations

- **Goal:** CRUD + typed Go views for principals/orgs/profiles; the identity substrate for policy and approvals.
- **Depends:** 20 · **Governing docs:** `docs/foundry/docs/architecture/domain-model.md`.
- **Steps:** `internal/identity` (principals/orgs) + `internal/profile` (load/save, config schema versioned); CLI `foundry profile create|show|list` and `foundry principal create`; seed fixtures: `dev-personal` (kind personal) and `dev-org` profiles used by all later e2e; validation: profile config parsed against JSONSchema `config/schemas/profile.schema.json`.
- **Outputs:** `internal/identity/*`, `internal/profile/*`, CLI cmds, schema file, seeds `test/fixtures/seed_profiles.go`, tests.
- **Acceptance:** invalid profile config rejected with pointer path; seeds idempotent.
- **Validation:** `go test ./internal/identity/... ./internal/profile/...`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 22 (FND-03) — Policy compiler v1 (non-weakening precedence)

- **Goal:** Layered config merge (platform → org → profile → workflow) producing `ResolvedPolicy{Digest}`; lower layers may tighten, never weaken; every override explained.
- **Depends:** 21 · **Governing docs:** `docs/foundry/docs/architecture/configuration-and-policy.md`; `docs/foundry/docs/security/authorization-model.md` (compiler-vs-PDP split).
- **Scope:** `internal/policy/compiler`; layer sources: embedded platform defaults `config/policy/platform.yaml` + org/profile rows.
- **Steps:** (1) Policy model: permissions allowlist, deployment modes per env, budget ceilings, executor allowlist, validation allowlist ref, notification classes, risk-tier controls. Each field annotated `tighten-only|fixed|free` in schema. (2) Merge algorithm: fold layers; tighten-only violation ⇒ compile error naming layer+field; produce `Resolved{Effective, Overrides []{Field, FromLayer, Old, New, Direction}} `+ sha256 digest. (3) Golden corpus ≥15 cases incl. attempted weakenings (must fail). (4) `foundry policy resolve --profile X` prints effective + explanations. (5) Property test: merge is deterministic and order-stable.
- **Outputs:** `internal/policy/{model.go,compiler.go,explain.go}` + goldens + CLI.
- **Acceptance:** all weakening fixtures fail compile; digest stable; explanations list every override.
- **Validation:** `go test ./internal/policy/... -run Golden -count=3 -race`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** no runtime decisions here (that's PDP). · **Status:** ☐ Not started

### Task 23 (FND-04) [P] — OPA PDP integration

- **Goal:** Runtime authorization: may principal X do action Y on resource Z given ResolvedPolicy digest D.
- **Depends:** 22 · **Governing docs:** authorization-model conformance tests (§ split).
- **Steps:** embed OPA as library (`github.com/open-policy-agent/opa/rego`) behind `policy.Decider` interface; input = {principal, action, resource, context, policy_digest}; rego policies in `config/policy/rego/` compiled at boot with digest pinning; conformance tests from the doc: (1) removing compiler breaks precedence tests even with PDP present, (2) decisions are pure functions of (request, digest), (3) weakened policy never reaches PDP.
- **Outputs:** `internal/policy/pdp/*`; rego files; conformance tests.
- **Acceptance:** three conformance tests green; decision latency <5ms p99 in bench.
- **Validation:** `go test ./internal/policy/pdp/... -race -bench Decide`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 24 (FND-05) — ApprovedPlan full chain (expiry, revocation, wave re-check)

- **Goal:** Complete C7: expiry enforced, revocation immediate, kernel re-checks at every task boundary.
- **Depends:** 21, 8 · **Governing docs:** `docs/foundry/docs/security/approval-and-provenance.md` §2.5.
- **Steps:** enforce `expires_at` on Load; `foundry plan revoke <id> --reason` sets revoked + audit row; kernel activity `RecheckApproval` before each task — revoked/expired ⇒ workflow FAILED `result_code: ADMISSION_REJECTED` with clean worktree release; repair-digest rule: any plan mutation produces new digest requiring re-approve (test).
- **Outputs:** provenance store updates; kernel `RecheckApproval` activity; CLI revoke; tests incl. mid-flight revocation e2e.
- **Acceptance:** revoking during task 2 of a 3-task plan halts before task 3 with correct terminal + audit entries.
- **Validation:** `go test ./internal/provenance/... ./internal/kernel/... -run Revoc` + e2e script.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 25 (FND-06) [P] — OIDC + WebAuthn strong-auth approvals (C12)

- **Goal:** Human approvals for High-risk actions require real identity + step-up; Telegram approval attempts are rejected with a pointer.
- **Depends:** 21 · **Governing docs:** approval-and-provenance §3; Blocker B5 (managed IdP; default Zitadel-class OIDC).
- **Steps:** (1) `internal/authn`: OIDC code flow for the API/CLI (`foundry login` device-code), session JWT (short-lived) bound to principal. (2) WebAuthn (go-webauthn) registration + assertion endpoints; approval endpoint `POST /v1/plans/{id}/approve` requires fresh WebAuthn assertion when Decision.Tier==H or profile=organization. (3) Approval record: {principal, method oidc+webauthn, assertion hash, at} appended to ApprovedPlan.Approvers. (4) Telegram command `approve` for H-tier returns "high-risk approval requires the secure surface: <url>" (C11 test). (5) Threat tests: replayed assertion rejected; expired session rejected.
- **Outputs:** `internal/authn/{oidc.go,webauthn.go,session.go}`; API handlers; CLI login; tests with a fake IdP (`test/fakes/oidc`).
- **Acceptance:** H-tier approve without WebAuthn ⇒ 403; with ⇒ recorded approver incl. method; Telegram path rejected.
- **Validation:** `go test ./internal/authn/... -race` + `bash test/approval_stepup_e2e.sh`.
- **Risk:** High · **Exec:** go-backend+security-review · **Rev:** **R3** · **Boundary:** no self-built crypto; libraries only. · **Status:** ☐ Not started

### Task 26 (FND-07) [P] — External-operation ledger + outbox (C9)

- **Goal:** Every side effect is reserved→executed→reconciled with an idempotency key; duplicates provably prevented.
- **Depends:** 20 · **Governing docs:** `docs/foundry/docs/architecture/external-operations.md`.
- **Steps:** `internal/ledger/extops`: `Reserve(kind,target,key,request) (OpID)` unique-key upsert; `MarkExecuted(opID, receipt)`; `Reconcile(opID, observed)`; kernel helper `WithExternalOp(key, fn)` wrapping activities — replay-safe (second call returns receipt). Reconciler job stub compares expected vs observed for kinds with a prober (git ref prober added Task 27). Metrics: `external_operation_divergence`, `duplicate_side_effect_prevented`.
- **Outputs:** `internal/ledger/extops/*`; kernel wrapper; reconciler skeleton `internal/ledger/reconcile.go`; tests (double-execute prevented under crash-injection: kill between execute and mark → replay returns receipt path).
- **Acceptance:** crash-injection test proves exactly-once effect; unique violation path clean.
- **Validation:** `go test ./internal/ledger/... -race -run CrashInjection -count=10`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 27 (FND-08) — GitHub SCM adapter with kernel-only push (C4)

- **Goal:** Mirror/fetch/worktree-source + branch push, callable exclusively from kernel activities, every push through the extops ledger.
- **Depends:** 26 · **Governing docs:** authority-model table; `docs/foundry/docs/workflows/multi-repository.md`.
- **Steps:** (1) `internal/scm` split: `scm/read` (Mirror, Fetch, ResolveRef — importable widely) and `scm/write` (PushBranch(ctx, repo, branch, expectedBase, newSHA) — **internal/kernel only**, enforced Task 28). (2) GitHub impl: token via secrets iface (Task 35 stub = env), go-git or gh CLI pinned — choose go-git; least-scope PAT documented. (3) Push protocol: lease on `repo:branch` → compare-and-swap (expectedBase check server-side via update refspec + verify) → receipt {beforeSHA, afterSHA, url} to ledger → release. (4) Fixture-based integration tests against a local bare repo + optional gated real-GitHub test (`RUN_GITHUB=1`, sandbox org repo). (5) `make e2e-github`: full plan run whose final kernel step pushes branch `foundry/e2e/<ts>` to fixture remote.
- **Outputs:** `internal/scm/read/*`, `internal/scm/write/github.go`; `make e2e-github`; tests.
- **Acceptance:** CAS push rejects on drift (test seeds a racing commit); receipts in ledger; e2e green.
- **Validation:** `go test ./internal/scm/... -race && make e2e-github`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** no PR APIs, no force-push code paths exist at all. · **Status:** ☐ Not started

### Task 28 (FND-09) [P] — Authority import-boundary fitness

- **Goal:** Compile-time + CI proof of C4: only kernel touches `scm/write`; agents get read-only.
- **Depends:** 27 · **Steps:** extend `cmd/fitlint`: `go list -deps` graph assertion — `internal/scm/write` imported only by `internal/kernel`; `internal/pec` (once created) imports neither `scm/write` nor `kernel`; seed violations; wire into `make fitness`.
- **Outputs:** fitlint rules + seeds.
- **Acceptance:** seeded violating file fails CI with named rule.
- **Validation:** `make fitness fitness-selftest`.
- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Status:** ☐ Not started

### Task 29 (FND-10) — Cost ledger v1: reservations + per-session caps (C19)

- **Goal:** Budgets enforced before spend; exhaustion pauses honestly.
- **Depends:** 20 · **Governing docs:** `docs/foundry/docs/operations/cost-accounting.md`.
- **Steps:** `internal/ledger/cost`: envelopes table `budgets(scope, scope_id, kind mission_monthly|provider|infra|experiment|reserve, ceiling_usd, period)`; `Reserve(scope, amount) error` atomic against ceiling−(reserved+incurred); `Incur`, `Reconcile`, `Release`; kernel pre-task hook estimates (packet-declared estimate or default table `config/cost-defaults.yaml`) and reserves; per-session cap: executor adapter budget context — exceeding cap cancels task with `WAITING/reason: budget` at workflow level and notification stub; shadow pricing hook for subscription executors (records `state=shadow`). `foundry cost show --scope mission:<id>`.
- **Outputs:** `internal/ledger/cost/*`; kernel hooks; CLI; tests (concurrent reservations never oversubscribe — property test).
- **Acceptance:** exhausted envelope ⇒ workflow WAITING budget, resumable after `foundry budget raise` (audited); oversubscription impossible under `-race` stress.
- **Validation:** `go test ./internal/ledger/cost/... -race -count=5`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 30 (FND-11) [P] — Telegram engine v1 (C11)

- **Goal:** Event classes, batching, flood control, nonce-bound low-risk commands. Never approvals for High-risk.
- **Depends:** 21 · **Governing docs:** `docs/foundry/docs/operations/telegram.md` (verify current Bot API limits at implementation; record in `docs/notes/telegram-limits.md`).
- **Steps:** `internal/notify`: event model {class P0..P3, workflow, text, dedupe_key}; per-chat token bucket + global bucket sized from verified limits; batcher: P2/P3 coalesce into digests (window configurable), P0 immediate; outbound via `0007_notifications` queue with retry/backoff + dead-letter; command router: `/status <wf>`, `/pause <wf>`, `/resume <wf>` with per-command nonce (issued in message, single-use, TTL 10m) and principal binding via chat-id registry; H-tier `/approve` rejected per Task 25. Soak test harness: 5k events burst → zero drops of P0, batching engaged, no 429s against a mock server enforcing limits.
- **Outputs:** `internal/notify/{engine.go,batch.go,bucket.go,commands.go,telegram.go}`; mock server `test/fakes/telegram`; soak test; limits note.
- **Acceptance:** soak green; nonce replay rejected; unknown chat rejected.
- **Validation:** `go test ./internal/notify/... -race && go run ./test/soak/telegram`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 31 (FND-12) [P] — Observability baseline

- **Goal:** OTel traces + Prometheus metrics for the catalog subset; dashboards seeded.
- **Depends:** 12 · **Governing docs:** `docs/foundry/docs/operations/observability-and-alerts.md` §1.
- **Steps:** `internal/observe`: OTel SDK wiring (foundryd + CLI opt-in), Prom exporter `/metrics`; instrument: workflow_completion_rate, evidence_rejection_rate, retry_rate, projection_lag_seconds (move from expvar), queue_depth (notifications), duplicate_side_effect_prevented, external_operation_divergence, cost_per_task (from ledger), provider_waiting_time (stub source); grafana JSON dashboards in `deploy/dashboards/`; compose gains prometheus+grafana profile `make up PROFILE=obs`.
- **Outputs:** `internal/observe/*`; instrumented call sites; dashboards; docs note mapping metric→owner→runbook stub.
- **Acceptance:** metrics visible in Grafana during `make skp-e2e`; each catalog-subset metric has HELP text matching the doc name.
- **Validation:** `curl -s :9090/metrics | grep -c foundry_` ≥ 8 + screenshot in evidence.
- **Risk:** Med · **Exec:** infra · **Rev:** R2 · **Status:** ☐ Not started

### Task 32 (FND-13) — Liveness supervisor, retry policy, PROVEN_BLOCKED (C22)

- **Goal:** Nothing stalls silently; bounded honest attempts end in a truthful terminal.
- **Depends:** 12 · **Governing docs:** `docs/foundry/docs/operations/disaster-recovery.md` (liveness); `docs/foundry/docs/workflows/recovery.md` (retry, honest completion).
- **Steps:** supervisor loop: scan projections for RUNNING without heartbeat/WAITING without wake_at or subscription ⇒ classify ORPHANED condition ⇒ repair (signal/reset per Temporal APIs) or escalate P1 notification; retry policy engine: per failure-classification budgets (retryable: 3 attempts exp backoff+jitter; no-progress detector: identical failure signature twice ⇒ stop); after budgets exhausted with evidence of impossibility (missing dependency, contradictory spec detected by rule set) ⇒ `FAILED/result_code: PROVEN_BLOCKED` + `next_action` for the human; chaos test: seed 5 stall modes (dead worker, stuck activity, missing wake, poisoned task, infinite retry attempt) — all detected <2×scan interval.
- **Outputs:** `internal/recovery/{supervisor.go,retrypolicy.go,blocked.go}`; chaos tests `test/chaos/liveness_test.go`.
- **Acceptance:** 5/5 stall modes detected + correct outcome; PROVEN_BLOCKED carries evidence refs + next_action.
- **Validation:** `go test ./test/chaos/ -run Liveness -race`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 33 (FND-14) [P] — Control-plane basics

- **Goal:** The Foundry protects itself: ingress limits, bounded queues, priority lanes, brownout.
- **Depends:** 31 · **Governing docs:** `docs/foundry/docs/operations/control-plane-protection.md`.
- **Steps:** API+webhook rate limits (token bucket per principal/IP); bounded intake queue (reject-with-429 over silent growth); priority lanes: recovery>delivery>notification>learning classes on worker task queues (separate Temporal task queues + worker slot allocation); brownout mode flag: sheds learning/memory queues first, keeps delivery+recovery — drill script proves shed order; dead-letter table + P1 alert.
- **Outputs:** middleware `internal/observe/limits.go`; queue config; `make drill-brownout`; tests.
- **Acceptance:** drill shows learning lane paused while delivery completes; DLQ alert fires on poisoned item.
- **Validation:** `make drill-brownout` + unit tests.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 34 (FND-15) — Rootless OCI executor sandbox

- **Goal:** Executors run in rootless containers: FS jail, a **narrow explicit egress allowlist** (proven to grant, not just deny), resource caps.
- **Depends:** 10 · **Governing docs:** `docs/foundry/docs/security/authorization-model.md` (runtime sandbox); §C container topology & network policy. **Resolved (Blocker B9 — hybrid):** this task launches a container from inside the `dev` container, so it needs a container engine `dev` doesn't have by default. Decision: the escape-attempt tests run in **two lanes** — a **bare-runner CI lane** (checkout only, no `dev` wrapper, direct host container engine) that is the **authoritative** signal and gates merges; and an **optional local lane** inside `dev` via a mounted host Docker socket (`-v /var/run/docker.sock:/var/run/docker.sock`), for convenience only, whose result never gates anything. No privileged nested-Docker-daemon path is used (it would weaken the outer container specifically to test the inner one's isolation).
- **Steps:** runner via rootless podman (or runc) launched from executor harness when `FOUNDRY_SANDBOX=oci`: image `deploy/images/executor.Dockerfile` (go, node, git, task tooling); mounts: workspace rw; **`gomod-cache` (and npm cache, if used) mounted read-only from the same named volumes `dev` already warms in Task 1**, so validation commands resolve packages locally instead of needing any runtime module-proxy access — this narrows the sandbox's real network need to almost nothing; everything else ro/absent. Network: default deny, allowlist = `config/sandbox-egress-allowlist.yaml` seeded with **only** the configured executor's own LLM provider endpoint(s) (e.g. `api.anthropic.com` when `FOUNDRY_EXECUTOR=claude-code`) — nothing else by default, because SCM writes, deployments, and billing are kernel-side activities that happen outside the sandbox (C4), so the sandbox itself never legitimately needs GitHub, Stripe, or Fly. cgroup caps (cpu/mem). Tests, both directions: (a) three escape-attempt tests — read /etc/shadow path, egress to a disallowed host, write outside workspace — all blocked; (b) **one legitimate-egress test — a request to the allowlisted destination (mocked provider endpoint) succeeds from inside the sandbox**, proving the allowlist actually grants what it's supposed to and the executor isn't silently broken by its own security boundary. Fake executor keeps subprocess mode for unit tests. CI: add a second GitHub Actions job `sandbox-tests` with no `dev` build step, running directly on the runner (`RUN_SANDBOX=1 go test ./internal/executor/sandbox/...` after a plain `setup-go`), required for merge; document the local socket-mount command in `README.md` under a "sandbox tests locally" note, explicitly marked non-authoritative.
- **Outputs:** `internal/executor/sandbox/oci.go`; Dockerfile; `config/sandbox-egress-allowlist.yaml`; cache-mount wiring; escape tests + legitimate-egress test (gated `RUN_SANDBOX=1`); `.github/workflows/ci.yaml` gains the bare-runner `sandbox-tests` job; README socket-mount note.
- **Acceptance:** 3/3 escape tests blocked **and** the legitimate-egress test passes, both in the bare-runner CI lane (authoritative); claude-code adapter functional inside sandbox using only the allowlisted endpoint, no cache-related network calls (gated test); local socket-mount lane documented and working but not required for merge.
- **Validation:** CI job `sandbox-tests` (bare runner) + optionally `RUN_SANDBOX=1 docker compose run --rm -v /var/run/docker.sock:/var/run/docker.sock dev go test ./internal/executor/sandbox/...` locally.
- **Risk:** High · **Exec:** infra+security-review · **Rev:** **R3** · **Status:** ☐ Not started

### Task 35 (FND-16) [P] — Secrets interface + file backend

- **Goal:** One secrets seam (`Get(ctx, scope, name)`), file backend now, Vault-ready later (Blocker B4).
- **Depends:** 20 · **Steps:** `internal/secrets`: interface + `filestore` (age-encrypted file `~/.foundry/secrets.age`, key from OS keychain or passphrase env for CI); scope model = profile-bound; audit read events; migrate existing env usages (GitHub token, Telegram token, executor auth) behind it; leak test: secrets never in logs/evidence (scanner over artifacts).
- **Outputs:** `internal/secrets/*`; migration of call sites; leak scanner in fitness.
- **Acceptance:** grep/scanner proves zero plaintext secrets in repo, logs, evidence fixtures.
- **Validation:** `go test ./internal/secrets/... && make fitness` (leak rule).
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 36 (FND-17) [P] — API server (CLI parity, consistency levels)

- **Goal:** `foundryd` HTTP API mirroring CLI: submit/approve/status/evidence/profiles, OIDC-protected, consistency levels honored.
- **Depends:** 21, 14 · **Steps:** chi/std-mux REST under `/v1`; OpenAPI spec `api/openapi.yaml` (source of truth, handlers generated or contract-tested); status endpoint `?consistency=fresh|projected`; authz via PDP (Task 23) per route; contract tests from the spec.
- **Outputs:** `internal/api/*`; `api/openapi.yaml`; contract tests.
- **Acceptance:** CLI reimplemented over API for status+submit paths (dogfood); spec-drift test fails on undocumented route.
- **Validation:** `go test ./internal/api/... -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 37 (FND-18) [P] — Documentation lint in CI

- **Goal:** V12 governance gates live in this repo's CI.
- **Depends:** 2 · **Governing docs:** `docs/foundry/docs/governance/documentation-rules.md`.
- **Steps:** extend fitness: superseded-term scan over `docs/**` (excluding mapped allowlist), duplicate mermaid D-ID detector, single-source contract heuristic (contract headings unique), link resolver already present — add anchor checking; **container-inventory lint: every `Dockerfile*` / `docker-compose*.y*ml` found in the repo must have a matching row in §C's container topology table** — an untracked Dockerfile fails CI by name, not just review; run on PRs touching docs.
- **Outputs:** `scripts/doclint/` + CI job.
- **Acceptance:** seeded violations (dup D-ID, dead anchor, stray TEN_X_BRANCHES_READY, **untracked Dockerfile**) each fail.
- **Validation:** `make fitness` + seeds.
- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Status:** ☐ Not started

### Task 38 (FND-19) — Projector v2: versioned projectors, rebuild tooling, lag alert (C3)

- **Goal:** Production-grade projection contract: versioned, migratable, observable.
- **Depends:** 14, 31 · **Governing docs:** data-consistency §2 (deploy-alongside → backfill → cutover).
- **Steps:** projector_version stamped per row; new-version rollout tool: run v(n+1) into shadow table, backfill from seq 0, checksum-compare window, atomic view swap; lag alert rule (Prom) with runbook `docs/runbooks/projection-lag.md`; API surfaces `X-Foundry-Consistency` header.
- **Outputs:** `internal/projection/versioning.go`; rollout CLI `foundry projection rollout`; alert+runbook.
- **Acceptance:** live rollout during running workflows loses zero updates (test with generator load).
- **Validation:** `bash test/projection_rollout_e2e.sh`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 39 (FND-20) — Backup/restore drill v0 (M1 exit)

- **Goal:** Prove PG backup/restore + Temporal namespace continuity; freeze M1 exit evidence.
- **Depends:** 20 · **Steps:** `make backup` (pg_dump custom format + evidence dir tar) / `make restore` into scratch env; drill: run plan → backup mid-flight → destroy env → restore → workflow continues (Temporal dev persistence noted; document self-hosted expectations for M2/Blocker B3); write `docs/notes/m1-exit-report.md` walking every M1 acceptance bullet with run links; tag `v0.2.0-foundation`.
- **Outputs:** backup/restore scripts; drill script; exit report; tag.
- **Acceptance:** M1 exit checklist all green: `make e2e-github`, WebAuthn gate e2e, notify soak, `projection rebuild`, audit chain verify (writer from 0008 + `foundry audit verify`), brownout drill.
- **Validation:** `make m1-exit` (meta-target chaining the above).
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Status:** ☐ Not started

---

## G. Track A — Venture MLS (Tasks 40–53) · runs in parallel with Track B

**Objective:** mission readiness → one opportunity/mockup → spec → generated PLAN → deterministic admission → build one small product → synthetic verification → personal-profile deployment → billing/activation observation → **one bounded autonomous improvement cycle**. **Non-goals:** real-money billing pre-maturity (C19/B6), Figma API (Task 80), multi-product portfolio (Task 81), full drift engine (Tasks 74–75). **Effort:** 6–12 wks (Low–Med). **Exit:** Task 53 checklist. **Rollback:** every deploy has rehearsed rollback (profile requirement); improvements revert via promotion records; mission pausable via `MISSION_PAUSED_FOR_HUMAN_GATE`.

### D-P4 — Venture MLS flow

```mermaid
flowchart TD
    CER[Task 41 Ceremony<br/>ReadinessArtifact] --> SEL[One opportunity<br/>or mockup fixture]
    SEL --> SPEC[Tasks 42-43 Spec synthesis<br/>O/I/A/U labels]
    SPEC --> PLANG[Task 44 PLAN generation]
    PLANG --> ADM[Task 45 Deterministic admission<br/>declared + detected effects]
    ADM --> BUILD[Task 46 Build from template]
    BUILD --> SYN[Task 48 Synthetic verification]
    SYN --> DEP[Task 47 Personal-profile deploy<br/>all requires green]
    DEP --> OBS[Tasks 49-50 Observe activation<br/>+ revenue reconcile]
    OBS --> IMP[Task 51 Bounded improvement<br/>self-generated plan]
    IMP --> ADM2{Inside envelope?}
    ADM2 -->|A0/A1| BUILD
    ADM2 -->|exceeds| HUM[Tier H human gate]
    OBS --> DIG[Task 52 Weekly veto digest]
```

### Task 40 (VEN-01) — MissionContract engine + result codes (C18)

- **Goal:** Missions are formal contracts with budgets, cadences, constraints, and exit semantics — never open loops.
- **Depends:** 21, 29 · **Governing docs:** `docs/foundry/docs/autonomy/mission-contract.md` (schema is authoritative — implement it field-for-field).
- **Scope:** `internal/mission`; migration `0009_missions.sql` (missions, mission_state, gate_events); mission evaluation is a Temporal workflow `MissionLoop` (cron-cadenced) that only orchestrates — product delivery still goes through `DeliverPlan`.
- **Steps:** (1) Parse/validate MissionContract YAML against JSONSchema generated from the doc's schema. (2) Evaluator: net-MRR rule (subs − refunds − cancellations − discounts; source = payment-provider ledger via Task 49 interface), confirmation window, min unrelated customers, refund threshold → emits mission result codes exactly as mapped in the doc (`MISSION_TARGET_REACHED`→SUCCEEDED etc. via `state` package). (3) Pause/terminate conditions wired: budget exhausted (Task 29 signal), payment-data-unavailable, unforeseen-human-gate. (4) `foundry mission create|show|pause|kill`. (5) Loop-contract records: every loop registers `{trigger,cadence,authority,budget,metrics,exit}` row — table `loop_contracts` — fitness rule: MissionLoop refuses to start without one.
- **Outputs:** `internal/mission/{contract.go,evaluator.go,workflow.go}`; `config/schemas/mission.schema.json`; migration 0009; CLI; table-driven evaluator tests (12+ scenarios incl. single-payment-not-success, window reset on dip).
- **Acceptance:** doc's USD-100 example contract round-trips; evaluator scenarios green; kill mid-loop ⇒ `CANCELLED/MISSION_KILLED` with clean product-state handoff note.
- **Validation:** `go test ./internal/mission/... -race`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Boundary:** no discovery/marketing logic. · **Status:** ☐ Not started

### Task 41 (VEN-02) — Mission Setup Ceremony + ReadinessArtifact (C17)

- **Goal:** Front-load every irreducible human gate; unattended runtime starts only after readiness passes.
- **Depends:** 40 · **Governing docs:** `docs/foundry/docs/autonomy/mission-setup-ceremony.md` (checklist is authoritative).
- **Steps:** ceremony checklist engine from the doc's four groups (identity/legal, money, infra/access, authority) as data `config/ceremony-checklist.yaml`; interactive `foundry mission ceremony <id>` walking items → each resolved (evidence ref) or explicitly deferred (reason + revisit_when); emits signed `MissionReadinessArtifact` (digest, approved_by) persisted + referenced by the deploy gate (Task 47 `mission-readiness-complete`); unforeseen-gate path: helper `EnterHumanGate(wf, action string)` ⇒ `WAITING/reason: unforeseen-human-gate`, checkpoint preserved, P1 notification with exact action, resume signal on completion, gate appended to artifact for next ceremony (test).
- **Outputs:** `internal/mission/ceremony.go`; checklist yaml; CLI; unforeseen-gate e2e test.
- **Acceptance:** mission start blocked until readiness pass; deferred required-item ⇒ readiness fail; unforeseen gate round-trip green.
- **Validation:** `go test ./internal/mission/ -run Ceremony && bash test/unforeseen_gate_e2e.sh`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 42 (VEN-03) [P] — Requirement→spec synthesizer with O/I/A/U labels (C16)

- **Goal:** Turn a requirement into a complete specification where every statement carries provenance labels.
- **Depends:** 21 · **Governing docs:** `docs/foundry/docs/workflows/mockup-to-delivery.md` §3–4 (labels + completeness list).
- **Steps:** `internal/spec`: `Requirement{ID, Text, Label Observed|Inferred|Assumed|Unresolved, Basis string}`; LLM synthesis call (executor-class LLM via provider seam) produces candidate requirements; **deterministic post-pass** enforces: completeness checklist coverage (loading/empty/error/validation/permissions/auth/persistence/APIs/responsive/a11y/analytics/billing/failure/NFR — missing section ⇒ auto-added as `Unresolved`), label present on every item, Assumed items must cite the policy default applied (`config/spec-defaults.yaml`); spec doc rendered to markdown with label badges; risk feed: counts of Unresolved by impact exported for Task 45.
- **Outputs:** `internal/spec/{model.go,synthesize.go,postpass.go,render.go}`; defaults yaml; golden fixtures (3 requirements → specs) with LLM replay cassettes (`test/cassettes/`) so tests are deterministic.
- **Acceptance:** postpass guarantees hold on adversarial LLM outputs (cassette with missing sections/labels); golden specs stable.
- **Validation:** `go test ./internal/spec/... -race` (cassette mode; live mode gated `RUN_LLM=1`).
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Boundary:** determinism lives in postpass, never trusted to the LLM. · **Status:** ☐ Not started

### Task 43 (VEN-04) [P] — Mockup ingestion v0: image/PDF → labeled spec inputs (C16)

- **Goal:** Mockup becomes a first-class entry: pixels in, labeled screen/flow/interaction model out.
- **Depends:** 42 · **Governing docs:** mockup-to-delivery §1–2 + D-28 pipeline (implement stages in order).
- **Steps:** ingestion (store under retention class visual-inputs — dir + metadata row); vision-LLM extraction per stage: screens/components → user-flow reconstruction → interaction/state analysis → a11y notes → backend/data/API inference; each stage output normalized to typed structs and **every derived item labeled** (Observed only if literally visible — deterministic heuristic: extraction confidence + stage type caps label strength; inference stages can never emit Observed); ambiguity classification feeds Task 42 synthesizer as pre-seeded requirements; fixtures: 2 mockup sets (simple landing+form PDF; 3-screen app PNGs) with golden extraction cassettes; high-impact Unresolved (auth, payment, destructive actions) flagged for admission raise.
- **Outputs:** `internal/spec/mockup/{ingest.go,stages.go,labels.go}`; fixtures + cassettes; retention wiring note.
- **Acceptance:** inference stages provably cannot emit Observed (unit test); fixture mockups produce specs whose auth/billing items are Unresolved/Assumed, never Observed.
- **Validation:** `go test ./internal/spec/mockup/... -race`.
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Boundary:** no Figma API (Task 80). · **Status:** ☐ Not started

### Task 44 (VEN-05) — PLAN generator from specification

- **Goal:** Spec → executable PLAN.md (Task 6 schema) with declared effects derived honestly from spec content.
- **Depends:** 42 · **Steps:** generator maps spec sections → plan tasks with commands/validation commands against the product template layout (Task 46); declared effects computed deterministically from spec content (billing section present ⇒ billing effect; schema entities ⇒ migration effect; etc. — table-driven mapping `config/effect-mapping.yaml`); generated plan **never sets a tier** (C6); output submitted through the normal `plan submit` path; golden: fixture specs → byte-stable plans.
- **Outputs:** `internal/spec/plangen.go`; mapping yaml; goldens.
- **Acceptance:** generated plans parse under strict mode; effect mapping covered by tests per mapping row; SelfClassified never set.
- **Validation:** `go test ./internal/spec/ -run PlanGen -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 45 (VEN-06) — AdmissionClassifier v1: detected effects + discrepancy raise (C6)

- **Goal:** Close the declared-vs-actual gap: deterministic effect detection from the repo/diff itself.
- **Depends:** 7, 27 · **Governing docs:** admission-tiers §1 (detected effects list).
- **Steps:** detectors over worktree/diff (pure, cassette-free): lockfile/dependency changes (go.mod/sum, package.json/lock), migrations dir touches, network destinations (new hosts in code/config via regex+AST), secret-scope references, billing code paths (paths under `/billing|stripe|payment/` + SDK imports), deploy target files, destructive SQL (DROP/TRUNCATE/DELETE without WHERE heuristic), permission manifests; classifier v1.1 merges declared+detected, computes `Discrepancies`, **any discrepancy raises tier one floor minimum** and H for billing/secret/destructive; ruleset version bump + old goldens preserved under version dir (decisions replayable per version); 20+ new golden fixtures incl. sneaky-diff cases (dependency added but undeclared ⇒ raise).
- **Outputs:** `internal/admission/detect/*.go`; `rules_v1_1.go`; goldens v1.1.
- **Acceptance:** every detector has positive+negative fixtures; sneaky-diff corpus all raised; determinism ×5 holds.
- **Validation:** `go test ./internal/admission/... -run 'Golden|Detect' -count=5 -race`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 46 (VEN-07) [P] — Product template repository

- **Goal:** The generatable small-SaaS skeleton every venture product starts from.
- **Depends:** 1 · **Governing docs:** overview repo model (product template concept); frontend per your stack preference; §C container topology (this is the _product template_ image lineage — a separate, self-contained Dockerfile owned by the generated product, never added to the platform's `deploy/docker-compose.yaml`).
- **Steps:** separate repo `delivery-foundry-product-template` (created + vendored as fixture tarball for tests): SvelteKit front + Go API back (or SvelteKit+endpoints monolith — decision: SvelteKit + Go API for parity with your stack), Postgres via env DSN, health endpoints `/healthz /readyz`, analytics hook (event table + `track()`), Stripe test-mode stubs (checkout session create, webhook receiver with signature verify — inert until Task 49 wires keys), Dockerfile + fly.toml template, Playwright smoke journey included, `make dev test e2e`; template instantiation tool `foundry product new --from-template --name X` (copier-style variable substitution).
- **Outputs:** template repo content under `templates/product/` in main repo + instantiation tool `internal/product/template.go`; instantiated-fixture CI test (instantiate → `make test` inside it).
- **Acceptance:** instantiate → unit + Playwright smoke green in CI container.
- **Validation:** `go test ./internal/product/ && bash test/template_instantiate_e2e.sh`.
- **Risk:** Med · **Exec:** web+go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 47 (VEN-08) — Personal deploy adapter + profile gate (C13)

- **Goal:** `deploy.Adapter` for Fly.io with the personal-autonomous-venture profile evaluated deterministically before every production deploy.
- **Depends:** 22, 46 · **Governing docs:** `docs/foundry/docs/autonomy/personal-venture-profile.md` (the `requires:` list is the gate, item-for-item).
- **Steps:** (1) `internal/deploy`: `Adapter{DeployPreview, DeployProduction, Rollback, Health}` + flyio impl (flyctl pinned version via secrets-provided token; app-per-product naming `foundry-<product>`); every deploy an extops ledger entry. (2) Profile gate evaluator: each `requires:` item → a checker func (mission-readiness-complete reads Task 41 artifact; spend-within-envelope reads Task 29; deterministic-verification-passed + synthetic-or-real-canary-passed read Task 48 records; rollback-rehearsed = last rehearsal timestamp within window; db-reversibility = migration lint; no-regulated-data = data-class flag; no-new-secret-scope = detector diff; health-checks-defined = template contract; allowlist = config). Single failure ⇒ downgrade to command mode + `WAITING/reason: human-approval` + notification (exact failing item named). (3) Rollback rehearsal automation: deploy N-1 → verify → redeploy N in preview env, recorded. (4) Gated live test against a scratch Fly org (`RUN_FLY=1`).
- **Outputs:** `internal/deploy/{adapter.go,flyio.go,gate.go,rehearse.go}`; migration `0010_deploys.sql` (deploy records + verification_mode + gate results jsonb); tests (gate matrix table-driven: 13 requires × pass/fail).
- **Acceptance:** gate matrix 26/26; failing item produces named downgrade; live gated deploy+rollback green.
- **Validation:** `go test ./internal/deploy/... -race` (+ `RUN_FLY=1` e2e evidence).
- **Risk:** **High** · **Exec:** go-kernel+infra · **Rev:** **R3** · **Boundary:** Fly only (adapter seam for others); no staging semantics beyond profile modes. · **Status:** ☐ Not started

### Task 48 (VEN-09) [P] — Synthetic verification suite (C21)

- **Goal:** Trustworthy verification at zero traffic, honestly labeled.
- **Depends:** 46 · **Governing docs:** admission-tiers §4 (CanarySignalPolicy + substitute list).
- **Steps:** `internal/verify/synthetic`: CanarySignalPolicy config per profile (min sessions/transactions/window/thresholds); traffic probe decides `verification_mode` (real-canary | synthetic-substitute | hybrid) — recorded on the deploy row, surfaced in notifications with explicit "synthetic — not real user validation" wording (C21 phrasing test); substitute battery orchestrated against preview/prod URL: Playwright journeys (template's), API contract tests, test-mode billing flow (checkout→webhook→state), webhook replay, synthetic load (vegeta, low rate), error injection (kill a dependency flag), migration+rollback rehearsal hook, post-deploy smoke monitor (5-min window); results = evidence bundle + pass/fail feeding Task 47 gate.
- **Outputs:** `internal/verify/synthetic/*`; policy config; battery runner; phrasing test.
- **Acceptance:** below-threshold traffic ⇒ mode=synthetic-substitute recorded + phrased; battery failure blocks deploy gate; hybrid path covered by fixture.
- **Validation:** `go test ./internal/verify/synthetic/... && bash test/synthetic_battery_e2e.sh` (against instantiated template).
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Status:** ☐ Not started

### Task 49 (VEN-10) — Stripe test-mode billing + revenue reconciler (C19)

- **Goal:** Billing wired in test mode; provider ledger is the authoritative revenue source, reconciled into the cost ledger.
- **Depends:** 29, 46 · **Governing docs:** cost-accounting; admission-tiers §3 (billing = Tier H changes; this task installs the _initial_ integration under human approval — seed an H-tier ApprovedPlan for it in fixtures to dogfood the rule).
- **Steps:** template wiring: checkout session, customer portal, webhook receiver (signature verify, idempotent event store `stripe_events`); reconciler job: pull balance transactions + subscriptions (test clock support) → compute net MRR components → write `revenue_reconciliation` rows (migration `0011_revenue.sql`) → feed Task 40 evaluator; failure mode: provider unreachable ⇒ mission `pause_when: payment-data-unavailable` path fires (test with Stripe mock); Stripe test fixtures via stripe-mock + recorded live-test-mode cassette (gated `RUN_STRIPE=1`).
- **Outputs:** `internal/billing/{stripe.go,webhook.go,reconcile.go}`; migration 0011; mocks + gated tests.
- **Acceptance:** webhook replay idempotent; reconciliation matches seeded test-clock scenario (3 subs, 1 refund) to the cent; unavailable-provider pause fires.
- **Validation:** `go test ./internal/billing/... -race` + gated live run evidence.
- **Risk:** High · **Exec:** go-backend · **Rev:** **R3** · **Boundary:** test mode only; zero live keys anywhere. · **Status:** ☐ Not started

### Task 50 (VEN-11) — Observation loop → mission evaluation

- **Goal:** Close the observe edge: product analytics + reconciled revenue evaluated on cadence, producing decide inputs.
- **Depends:** 40, 49 · **Steps:** `MissionLoop` observe tick: pull analytics events (template's event table via product DSN registry) + latest reconciliation → compute progress metrics (activation, conversion, MRR trajectory, cost-to-date from ledger) → persist `mission_observations` → evaluate contract (Task 40) → emit decide record {continue|improve|pivot|kill-candidate} where pivot/kill are **proposals** requiring policy (personal profile: kill within no-progress rule auto; pivot = H) — mapping table in mission config; notifications P2 digest.
- **Outputs:** `internal/mission/observe.go`; migration append to 0009; decide-policy config; tests (trajectory fixtures → correct decide records).
- **Acceptance:** no-progress-cycles counter triggers per contract; decide records match fixtures; observation is read-only (no side effects — fitness note).
- **Validation:** `go test ./internal/mission/ -run Observe -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 51 (VEN-12) — Bounded autonomous improvement cycle

- **Goal:** The self-prompt loop, governed: observation → generated improvement plan → detected-effects admission → envelope check → build/deploy → promotion record.
- **Depends:** 45, 47 · **Governing docs:** venture-loop front matter; admission-tiers (self-generated = provenance, not authorization).
- **Steps:** improvement generator: decide=improve + observation context → LLM proposes ONE bounded change (prompt constrains: single concern, reversible, inside repo) → Task 44 plangen path → submitted with `creator_principal=service:mission-loop` (provenance) → Task 45 classification (fixtures ensure: copy tweak ⇒ A0/A1 auto-admit; new dependency ⇒ raised; billing touch ⇒ H halt) → envelope check = profile auto_tiers + budget reservation → DeliverPlan → deploy via gate → `promotions` row (migration `0012_promotions.sql`: change ref, plan digest, before/after metrics slots, rollback ref, level=plan-cycle) ; hard bound: max 1 in-flight improvement per product (lease) + per-cycle budget cap; out-of-envelope e2e fixture proves halt at H with notification.
- **Outputs:** `internal/mission/improve.go`; migration 0012; cassettes for generator; e2e `test/improvement_cycle_e2e.sh` (in-envelope auto + out-of-envelope halt).
- **Acceptance:** full in-envelope cycle with **zero human touches** in test; H fixture halts pre-build; promotion row complete with rollback ref.
- **Validation:** `bash test/improvement_cycle_e2e.sh`.
- **Risk:** **High** · **Exec:** go-kernel+integration · **Rev:** **R3** · **Boundary:** one bounded cycle; no L0/L1 parameter promotion (Tasks 74–75). · **Status:** ☐ Not started

### Task 52 (VEN-13) [P] — Weekly veto digest v0 (C11/C20 precursor)

- **Goal:** Non-blocking governance: weekly Telegram digest of promotions with 24h veto window; freeze-condition stubs.
- **Depends:** 30, 51 · **Governing docs:** `docs/foundry/docs/autonomy/cumulative-drift-governance.md` §3.
- **Steps:** digest job (cron workflow): promotions in window → message per doc spec (change list, before/after metrics, budget consumption placeholder, rollback links `/rollback <promo-id>` nonce-commanded) → 24h veto window record; veto command executes rollback via kernel (deploy N-1 + revert plan) + marks promotion vetoed + learning-evidence row; no-veto auto-continue; freeze stubs: rollback-chain depth >2 or vetoed-twice-same-target ⇒ improvement lease frozen until `foundry promotions unfreeze` (audited).
- **Outputs:** `internal/notify/digest.go`; veto command path; freeze logic; e2e (digest→veto→rollback verified).
- **Acceptance:** veto within window rolls back and freezes correctly; expired veto ignored; digest never blocks the loop (loop continues during window — test).
- **Validation:** `bash test/veto_digest_e2e.sh`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 53 (VEN-14) — Venture MLS e2e (Track A exit)

- **Goal:** Prove Track A exit: the whole loop, unattended, on fixtures + one gated live run.
- **Depends:** 41,43,44,47,48,49,50,51,52 · **Steps:** `make e2e-venture` = ceremony(fixture answers) → mockup fixture → spec → plan → admission → build template product → synthetic battery → gated deploy (fly scratch or local docker "prod" for CI) → stripe-mock activation+payment → observation → one auto improvement → digest capture; assertions: zero human interaction between readiness-pass and digest except the seeded H fixture; human-touch counter metric = 0 on happy path; write `docs/notes/track-a-exit-report.md`; tag `v0.3.0-venture-mls`.
- **Outputs:** e2e harness `test/e2e/venture/*`; Make target; exit report; tag.
- **Acceptance:** CI-mode e2e green 3 consecutive runs; live gated run evidence archived.
- **Validation:** `make e2e-venture`.
- **Risk:** High · **Exec:** integration · **Rev:** **R3** · **Status:** ☐ Not started

---

## H. Track B — 10x MLS (Tasks 54–63) · runs in parallel with Track A

**Objective:** approved PLAN → provenance validation → one repository → one atomic implementation group → deterministic checks → PEC recommendation → Branch Integrator → push to an **existing** 10x branch → `status: SUCCEEDED, result_code: TEN_X_BRANCH_HANDOFF_READY`. **Non-goals (C15, test-enforced):** no PR creation, no merge, no staging, no deployment anywhere in this workflow; no multi-repo saga (Task 78); no deep Jira/Confluence adapters (reference validation stubs only). **Effort:** 4–8 wks (Medium). **Rollback:** integrator never force-pushes; irreconcilable divergence ends honestly as `PROVEN_BLOCKED` with human `next_action`.

### D-P5 — 10x MLS flow

```mermaid
flowchart LR
    AP[Approved PLAN.md<br/>strong-auth org approvals] --> PV[Task 55 Provenance validation<br/>source digests + refs]
    PV --> WT[Isolated worktree<br/>one repository]
    WT --> AG[Task 57 One atomic group]
    AG --> DC[Deterministic checks]
    DC --> PEC[Task 56 PEC recommendation<br/>proposal only]
    PEC --> BI[Task 58 Branch Integrator<br/>lease + fencing + drift guard]
    BI --> PUSH[Direct push to existing 10x branch]
    PUSH --> HR[SUCCEEDED<br/>TEN_X_BRANCH_HANDOFF_READY]
    HR -.never.-> NO[No PR, no merge,<br/>no staging, no deploy]
```

### Task 54 (TX-01) — Organization profile + governance pack (C14)

- **Goal:** The org profile: stricter policy layer that only tightens platform defaults.
- **Depends:** 22 · **Governing docs:** `docs/foundry/docs/architecture/configuration-and-policy.md`; profile taxonomy.
- **Steps:** org profile fixture `config/profiles/organization-10x.yaml`: deployment all `command`; auto_tiers `[A0]` only; approvals required roles [engineering, qa] configurable; executor allowlist narrowed; notification classes org-channel; compile through Task 22 (weakening attempts in tests must fail); `foundry profile create --kind organization` path; PDP rules for org actions (approve, push authorization) added to rego pack.
- **Outputs:** profile yaml + schema extension; rego additions; tests (tighten-only proofs; A2 grant attempt at org layer fails compile).
- **Acceptance:** org profile compiles; every weakening fixture fails with named field; PDP denies push authorization to non-kernel principals.
- **Validation:** `go test ./internal/policy/... -run Org -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 55 (TX-02) — Org plan provenance validation (C7, C12)

- **Goal:** Organization plans prove where they came from and who approved them, strongly.
- **Depends:** 24, 25 · **Governing docs:** `docs/foundry/docs/security/approval-and-provenance.md` §3.
- **Steps:** extend admission for org profile: (1) source repo+revision validation — plan declares `{repo, revision, source_digests[]}`; validator fetches (scm/read) and verifies each digest against the revision's tree; mismatch ⇒ `ADMISSION_REJECTED`. (2) Reference checks: PRD/RFC/test refs validated by pluggable `RefValidator` (v1: URL-reachable + pattern registry; Jira/TestRail deep validation = stub interface with TODO ticket note, Blocker note). (3) Approver-role enforcement: required roles from profile matched against Approvers (each WebAuthn-backed via Task 25); missing role ⇒ reject naming it. (4) CLI ergonomics: `foundry plan submit --org --repo <url> --rev <sha>` auto-computes source digests. (5) e2e: tampered source digest, missing QA approver, valid path.
- **Outputs:** `internal/provenance/org.go`; ref validator registry; CLI flags; migration `0013_org_provenance.sql` (source records); e2e `test/org_provenance_e2e.sh`.
- **Acceptance:** 3 e2e scenarios correct; every rejection names the failing check; approvals recorded with method.
- **Validation:** `go test ./internal/provenance/ -run Org && bash test/org_provenance_e2e.sh`.
- **Risk:** **High** · **Exec:** go-kernel+security-review · **Rev:** **R3** · **Status:** ☐ Not started

### Task 56 (TX-03) [P] — PEC v1: wave/remediation proposals + prohibition tests (C5)

- **Goal:** The Plan Execution Coordinator as a pure proposal engine — provably incapable of side effects.
- **Depends:** 6 · **Governing docs:** `docs/foundry/docs/architecture/authority-model.md` (authority table + prohibitions, verbatim).
- **Steps:** `internal/pec`: `ProposeWaves(doc plan.Document) (WaveProposal, error)` — topological sort on DependsOn into dependency-honest waves, deterministic tie-break by task ID; `ProposeRemediation(failed TaskRef, records []verify.CommandRecord, summaries []executor.Summary) Remediation{Suggestion, Confidence, Evidence []string}` (LLM-assisted with cassettes; suggestions only); `ReportProgress(transitions) PlanProgress`. Package doc.go states prohibitions; **enforcement**: fitlint rule (extends Task 28) — `internal/pec` may import only `plan`, `state`, `verify` types, `executor` Summary type; importing `kernel`, `scm`, `ledger`, `provenance`, database drivers, or `net/http` fails CI; API-shape test: no exported function returns anything the kernel executes without its own authorization (proposal types carry no capability handles). Kernel integration: `DeliverPlan` optionally consults PEC for wave order — kernel validates proposal against its own dependency check before use (distrust test: malformed proposal ignored, kernel falls back to sequential).
- **Outputs:** `internal/pec/{waves.go,remediate.go,progress.go,doc.go}`; fitlint rule + seeds; kernel consult path; cassettes; property test (waves respect all edges, ×1000 random DAGs).
- **Acceptance:** prohibition seeds fail CI; malformed-proposal distrust test green; wave property test green.
- **Validation:** `go test ./internal/pec/... -race && make fitness`.
- **Risk:** **High** (constitution-bearing) · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 57 (TX-04) — Atomic group model + change-set manifest

- **Goal:** The unit 10x pushes: one coherent, reviewable, deterministic-checked group of commits.
- **Depends:** 6 · **Governing docs:** `docs/foundry/docs/workflows/ten-x-branch.md` (atomic group semantics).
- **Steps:** `AtomicGroup{ID, PlanTaskIDs []string, Commits []SHA, Manifest ChangeSet}`; `ChangeSet{Files []{Path, Action, BlobSHA}, Tests []string, ValidationRecords []ref}` computed from worktree diff post-validation; manifest embedded in a trailer on the group's tip commit (`Foundry-Changeset: <digest>`) for downstream traceability; squash-vs-preserve policy from org profile (default: preserve task commits, group boundary = empty marker commit? No — default: one commit per plan task, group recorded in manifest only); guard: group touching files outside plan-declared scope ⇒ FAILED `policy-violation` (test).
- **Outputs:** `internal/kernel/atomicgroup.go`; manifest schema; scope-guard tests.
- **Acceptance:** manifest digest reproducible from repo state; out-of-scope file fixture fails correctly.
- **Validation:** `go test ./internal/kernel/ -run AtomicGroup -race`.
- **Risk:** Med · **Exec:** go-kernel · **Rev:** R2 · **Status:** ☐ Not started

### Task 58 (TX-05) — Branch Integrator: push queue, lease, fencing, receipts (C4)

- **Goal:** The only component that writes to shared 10x branches — serialized, fenced, receipted.
- **Depends:** 27, 57 · **Governing docs:** authority-model; ten-x-branch (integration rules).
- **Steps:** `internal/kernel/integrator`: per-branch FIFO queue (PG table `integration_queue` + advisory lock); protocol per item: acquire branch lease (fencing token) → fetch remote head → verify expectedBase (drift check → Task 59) → fast-forward-only apply of atomic group commits onto branch → CAS push via scm/write with token → receipt {branch, beforeSHA, afterSHA, groupID, manifestDigest} to extops ledger → release; concurrency test: 3 workflows racing same branch serialize with zero lost updates (assert linear history); force-push impossible (no code path — negative test greps + API-shape test).
- **Outputs:** `internal/kernel/integrator/*`; migration `0014_integration_queue.sql`; race e2e vs local bare remote.
- **Acceptance:** 3-way race linearizes; receipts complete; stale fencing token rejected (test kills holder mid-push).
- **Validation:** `go test ./internal/kernel/integrator/... -race -count=5 && bash test/integrator_race_e2e.sh`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 59 (TX-06) [P] — Drift guard + requeue + PROVEN_BLOCKED divergence path

- **Goal:** Humans push to shared branches too; the integrator behaves honestly around them.
- **Depends:** 58 · **Steps:** drift on expectedBase ⇒ requeue with rebase-attempt policy (org-config: `rebase-clean-only` default — clean rebase of group onto new head + re-run deterministic checks; conflict ⇒ bounded retries then `FAILED/result_code: PROVEN_BLOCKED` with next_action "manual rebase of group <id> onto <sha>", worktree preserved for handoff); notification P1 with receipt links; tests: concurrent human commit (fixture) → clean rebase path; conflicting human commit → PROVEN_BLOCKED path with preserved worktree.
- **Outputs:** `internal/kernel/integrator/drift.go`; policy knob; both-path e2e.
- **Acceptance:** both fixtures land exact terminal + artifacts; re-run of validation after rebase enforced (no stale-check push — test).
- **Validation:** `bash test/integrator_drift_e2e.sh`.
- **Risk:** Med · **Exec:** go-kernel · **Rev:** R2 · **Status:** ☐ Not started

### Task 60 (TX-07) — Handoff terminal + notification (C15)

- **Goal:** The 10x workflow's only success shape, emitted precisely.
- **Depends:** 58 · **Steps:** terminal mapping in the 10x workflow variant of `DeliverPlan` (`TenXDeliver`): all groups pushed ⇒ `SUCCEEDED` + `result_code: TEN_X_BRANCH_HANDOFF_READY` (via `state` constants only); handoff notification (org channel): branch(es), receipts, manifest digests, evidence links, "no PR/merge/deploy was performed" statement; projection + CLI render the pair correctly; alias input still normalizes (Task 5) but is never emitted (assert in e2e).
- **Outputs:** `internal/kernel/tenx_workflow.go`; notification template; terminal tests.
- **Acceptance:** e2e emits exact status+result_code; notification contains receipts; grep proves no alias emission.
- **Validation:** `go test ./internal/kernel/ -run TenX -race`.
- **Risk:** Med · **Exec:** go-kernel · **Rev:** R2 · **Status:** ☐ Not started

### Task 61 (TX-08) [P] — Prohibited-operations enforcement tests (C15)

- **Goal:** "No PR, merge, staging, deploy in 10x" is proven, not promised.
- **Depends:** 60 · **Steps:** three layers: (1) code absence — fitlint: `TenXDeliver` call graph (go/callgraph) reaches no symbol matching PR/merge/deploy surfaces (scm has none for PRs by design; deploy pkg unreachable from tenx path); (2) runtime — e2e runs against instrumented fakes recording every external call; assert allowed-call set exactly {fetch, push, notify, ledger, evidence}; (3) seeds — a branch adding a PR-creation call into the tenx path must fail CI. Wire as `make fitness-tenx` into `make fitness`.
- **Outputs:** callgraph fitlint rule; instrumented-fake harness; seeds.
- **Acceptance:** seed fails; runtime allowed-set assertion green in `make e2e-tenx`.
- **Validation:** `make fitness-tenx && make e2e-tenx`.
- **Risk:** Med · **Exec:** infra+go-kernel · **Rev:** **R3** (constitution-bearing) · **Status:** ☐ Not started

### Task 62 (TX-09) [P] — Bitbucket adapter (optional; Blocker B2)

- **Goal:** scm/read+write parity for Bitbucket ahead of live Company-style use.
- **Depends:** 58 · **Steps:** implement `scm/read` + `scm/write` for Bitbucket Cloud (REST + git over https; app password/token via secrets); same CAS-push semantics (Bitbucket lacks server-side CAS on push — emulate: fetch-verify-push under lease + immediate post-push verify, divergence ⇒ receipt flag + requeue); adapter contract test suite (shared table for github|bitbucket|localgit) — write once here, run for all.
- **Outputs:** `internal/scm/write/bitbucket.go` + read impl; shared contract tests `internal/scm/scmtest/`.
- **Acceptance:** contract suite green for all three backends (bitbucket gated `RUN_BITBUCKET=1`).
- **Validation:** `go test ./internal/scm/... -race` (+ gated live).
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 63 (TX-10) — 10x MLS e2e + live dry-run (Track B exit)

- **Goal:** Prove Track B exit end-to-end, then once against a real repo clone.
- **Depends:** 55,56,59,60,61 · **Steps:** `make e2e-tenx` = org profile → strong-auth-approved fixture PLAN (two atomic groups) → provenance validation → worktrees → groups → checks → PEC waves consulted → integrator (with seeded human-commit drift on group 2) → pushes to pre-existing `10x/initiative-fixture` branch → exact terminal + notification capture → prohibition runtime assertion; live dry-run script: clone of a real repo (your choice), throwaway 10x branch, real PLAN.md you author + approve via WebAuthn — evidence archived; `docs/notes/track-b-exit-report.md`; tag `v0.3.0-tenx-mls`.
- **Outputs:** e2e harness `test/e2e/tenx/*`; dry-run script + checklist; exit report; tag.
- **Acceptance:** CI e2e green ×3; dry-run evidence (receipts, branch URL, terminal pair) archived; zero prohibited calls in runtime log.
- **Validation:** `make e2e-tenx`.
- **Risk:** High · **Exec:** integration · **Rev:** **R3** · **Status:** ☐ Not started

---

## I. Milestone M2 — Operational Hardening (Tasks 64–73)

**Objective:** safe to run unattended; safe to trust with money and shared branches. **Non-goals:** no new product capability; no learning features. **Effort:** 4–8 wks (Medium). **Exit (Task 73):** chaos suite green 7 consecutive days on staging; 72-hour unattended soak with zero silent stalls; security findings resolved or signed off. **Note:** each M2 card is complete but carries a **Refinement gate**: re-verify its assumptions against M1/track telemetry at milestone entry — that check is part of the task, not a gap.

### Task 64 (HRD-01) — Fault-injection suite

- **Goal:** Systematic chaos: every failure mode from `docs/foundry/docs/workflows/recovery.md` + `operations/disaster-recovery.md` induced and survived.
- **Depends:** 53 or 63 (whichever exits first) · **Steps:** harness `test/chaos/` scenarios: worker kill matrix (each activity boundary), Temporal outage (stop container mid-plan; resume), PG outage (projections stale-labeled, workflows continue, catch-up verified), provider 429/5xx storms (executor + Stripe + Fly fakes), poisoned task (DLQ + alert), clock skew; each scenario asserts: correct status/reason, no duplicate side effects (ledger proof), recovery within SLO, alert fired; nightly CI job. Refinement gate: pull real M1 incident notes into scenario list.
- **Outputs:** chaos harness + scenario table `test/chaos/SCENARIOS.md`; nightly workflow.
- **Acceptance:** all scenarios green ×7 nights; every scenario maps to a runbook.
- **Validation:** `make chaos`.
- **Risk:** High · **Exec:** go-kernel+infra · **Rev:** **R3** · **Status:** ☐ Not started

### Task 65 (HRD-02) [P] — Backpressure + fairness complete

- **Goal:** Full control-plane protection per `docs/foundry/docs/operations/control-plane-protection.md`.
- **Depends:** 33 · **Steps:** per-profile concurrency quotas (workflows, runners, admissions); fairness scheduler test (greedy profile cannot starve others — load test with 2 profiles); load shedding tiers wired to brownout; DB pool ceilings + statement timeouts; admission backpressure (intake pauses before execution collapse — threshold from Task 31 metrics); soak `make soak-fairness`.
- **Outputs:** quota config + enforcement; fairness soak; runbooks.
- **Acceptance:** starvation test bounded (p95 wait for profile B under profile A flood < threshold); shed order proven.
- **Validation:** `make soak-fairness`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 66 (HRD-03) [P] — Retention/PII enforcement (UU PDP)

- **Goal:** `docs/foundry/docs/security/data-retention-and-privacy.md` classes enforced in code.
- **Depends:** 20 · **Steps:** retention registry (class per table/store from the doc's table, config `config/retention.yaml`); TTL sweeper jobs per class; deletion cascades: deleting a source cascades to memory rows, vector entries (placeholder store), caches, projections (test per class); legal-hold flag suspends deletion with audit; DSR endpoints (access/export/delete) for venture products with UU PDP timeline fields; backup expiry alignment check; access logging on customer/billing/audit/memory classes.
- **Outputs:** sweeper `internal/retention/*`; DSR API; cascade tests per class; migration `0015_retention.sql` (holds, dsr_requests).
- **Acceptance:** cascade matrix green; hold blocks sweep; DSR export complete for fixture user.
- **Validation:** `go test ./internal/retention/... -race`.
- **Risk:** High · **Exec:** go-backend+security-review · **Rev:** **R3** · **Status:** ☐ Not started

### Task 67 (HRD-04) [P] — Audit hash-chain verify + tamper drill

- **Goal:** The audit log proves its own integrity.
- **Depends:** 20 · **Steps:** `foundry audit verify` walks chain (prev_hash→hash) full + incremental (checkpoint anchors every 10k rows, anchor digest also mirrored to evidence store); tamper drill: mutate a row in scratch DB ⇒ verify names exact seq; scheduled verify job + alert; export format for external anchoring (optional note).
- **Outputs:** verify CLI + job; drill script; anchors.
- **Acceptance:** tamper detected at exact row; incremental verify O(delta).
- **Validation:** `bash test/audit_tamper_drill.sh`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 68 (HRD-05) [P] — SLO alerts + runbooks (full catalog)

- **Goal:** Every metric in `observability-and-alerts.md` §1 gets: recording rule, threshold, alert, owner, runbook, dashboard slot.
- **Depends:** 31 · **Steps:** complete instrumentation gaps (auto_admission_rate, auto_deployment_rate, auto_promotion_rollback_rate, mission_target_attainment, time_to_first_revenue, unattended_runtime_median, human_intervention_rate from Task 53 counters); Prometheus rules files; Alertmanager routes (Telegram P1 channel); `docs/runbooks/<metric>.md` one per alert (symptom, verify, mitigate, escalate); dashboard update; payload-limit conformance test (seed 3MB artifact into workflow arg path ⇒ rejected with pointer to object store).
- **Outputs:** rules + routes; 19 runbooks; dashboards; payload test.
- **Acceptance:** alert-fires drill per rule (amtool test or induced); runbook lint (sections present).
- **Validation:** `make alerts-drill`.
- **Risk:** Med · **Exec:** infra · **Rev:** R2 · **Status:** ☐ Not started

### Task 69 (HRD-06) — Cost reconciliation jobs + cap proofs (C19)

- **Goal:** Ledger truthfulness at production grade: reserved vs incurred vs reconciled closes, caps provably bind.
- **Depends:** 29, 49 · **Steps:** nightly reconciliation: provider usage pulls (executor token telemetry, Stripe fees, Fly billing API where available; else manual-import path) → match to reservations → release/flag variances >x%; per-session cap proof: adversarial executor fixture attempts overspend ⇒ cancelled at cap with `WAITING/budget`; shadow-cost report for subscription executors; monthly mission economics statement (revenue, gross margin, net contribution, cost/cycle, payback) rendered to notes + digest.
- **Outputs:** reconciliation jobs; variance alerts; adversarial test; statement renderer.
- **Acceptance:** seeded variance flagged; cap proof green ×10; statement matches fixture ledger to the cent.
- **Validation:** `go test ./internal/ledger/... -run 'Reconcile|Cap' -race -count=10`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 70 (HRD-07) — Security review + prompt-injection red-team

- **Goal:** Adversarial pass over the whole authority surface before unattended trust.
- **Depends:** 34, 64 · **Governing docs:** `docs/foundry/docs/security/authorization-model.md` (threat model + injection defense).
- **Steps:** red-team corpus: injection payloads embedded in plan text, repo files, mockups, executor summaries, Telegram messages — assert tool-authorization boundary holds (executor cannot mint approvals, cannot reach scm/write, cannot exfiltrate secrets — sandbox + allowlist proofs); sandbox escape re-run (Task 34 suite extended); dependency audit (govulncheck + SBOM); secrets posture review; findings tracked `docs/notes/security-review-m2.md` with fix tasks spawned in-scope or accepted+signed; **R4 human sign-off required** to close.
- **Outputs:** red-team corpus `test/redteam/`; review report; fixes.
- **Acceptance:** corpus fully inert; zero criticals open; sign-off recorded.
- **Validation:** `make redteam && govulncheck ./...`.
- **Risk:** High · **Exec:** security-review · **Rev:** **R4** · **Status:** ☐ Not started

### Task 71 (HRD-08) [P] — DR drill automation

- **Goal:** Restore-from-backup as a rehearsed monthly muscle, not a document.
- **Depends:** 39 · **Steps:** automate Task 39 drill against production-shaped env (self-hosted Temporal per Blocker B3 decision — implement docker-compose "prod" profile with Temporal+Cassandra-or-PG persistence); scheduled monthly CI job: backup → destroy → restore → in-flight workflow continues → report; RTO/RPO measured and recorded vs targets in disaster-recovery doc.
- **Outputs:** prod-profile compose; drill workflow; RTO/RPO report format.
- **Acceptance:** drill green; measured RTO/RPO within declared targets or targets amended honestly.
- **Validation:** `make dr-drill`.
- **Risk:** Med · **Exec:** infra · **Rev:** R2 · **Status:** ☐ Not started

### Task 72 (HRD-09) [P] — Telegram hardening: replay/nonce fuzz + flood soak

- **Goal:** The command surface survives abuse.
- **Depends:** 30 · **Steps:** fuzz command router (nonce replay, malformed, TTL boundaries, cross-chat nonce theft, unicode tricks); 24h flood soak vs mock enforcing verified limits; rate-limit bypass attempts; kill-switch command `/freeze` (P0, principal-bound) added + tested.
- **Outputs:** fuzz corpus + go-fuzz targets; soak job; freeze command.
- **Acceptance:** zero fuzz crashes/bypasses in 1h fuzz; soak drop-free for P0/P1.
- **Validation:** `go test -fuzz=FuzzCommands -fuzztime=1h ./internal/notify/ && make soak-telegram`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 73 (HRD-10) — Versioned release + upgrade path (M2 exit)

- **Goal:** Ship as software: versioned releases, safe upgrades, the M2 gate.
- **Depends:** 64,65,66,67,68,69,70,71,72 · **Governing docs:** §C container topology (this produces the _release_ image lineage — a slim runtime image distinct from `dev`; `dev` is never what ships). · **Steps:** release process doc + `make release` (goreleaser: binaries, image, SBOM, signed checksums); Temporal workflow versioning policy (`workflow.GetVersion` patches; replay tests mandatory in CI for kernel changes — enforce via CI path filter); upgrade drill: N-1 → N with in-flight workflows across the boundary; 72-hour unattended soak on staging (both tracks looping fixtures) — human_intervention counter must be 0 outside seeded gates; `docs/notes/m2-exit-report.md`; tag `v0.4.0-hardened`.
- **Outputs:** release tooling; versioning policy doc; upgrade drill; soak report; tag.
- **Acceptance:** upgrade drill zero-loss; soak criteria met; all C-articles re-verified in report.
- **Validation:** `make release-dryrun && make upgrade-drill && make soak-72h`.
- **Risk:** High · **Exec:** infra+go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

---

## J. Milestone M3 — Capability Evolution, Learning, Broader Providers (Tasks 74–83)

**Objective:** governed self-adaptation (L0/bounded-L1), multi-repo 10x, provider breadth, portfolio scaling. **Non-goals (standing):** L2+ auto-promotion stays human-gated absent new evidence; no org-wide skill auto-promotion; no cross-profile memory. **Effort:** quarters (Low confidence — **mandatory replan of this section at M2 exit using real telemetry**; each card's Refinement gate is part of the task). **Exit:** one month of governed autonomous improvement on the live venture, drift budget respected, ≥1 exercised veto/rollback proving the brake.

### Task 74 (EVO-01) — L0 auto-promotion pipeline

- **Goal:** Runtime parameter tuning (retry timing, batch sizing, compaction thresholds, routing weights within bounds) promoted automatically after replay→shadow→canary evaluation.
- **Depends:** 73 · **Governing docs:** `docs/foundry/docs/autonomy/cumulative-drift-governance.md` §1; `docs/foundry/docs/workflows/capability-evolution.md`.
- **Steps:** tunable registry `config/tunables.yaml` (name, bounds, scope, evaluation metric); candidate generator (from learning evidence rows); pipeline: replay eval (recorded histories) → shadow (parallel non-authoritative run) → canary (one profile, bounded window) → promote with previous value retained + rollback ref → `promotions` row level=L0; every promotion inside declared bounds only (out-of-bounds candidate rejected — test); Refinement gate: seed tunables list from M1/M2 telemetry pain points.
- **Outputs:** `internal/evolve/{tunables.go,pipeline.go}`; registry; pipeline tests with fixture metrics.
- **Acceptance:** out-of-bounds rejected; failed canary auto-reverts; promotion reversible in one command.
- **Validation:** `go test ./internal/evolve/... -race`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 75 (EVO-02) — CumulativeChangeBudget + freeze + full digest (C20)

- **Goal:** Drift itself budgeted; the weekly digest becomes real governance analytics.
- **Depends:** 74 · **Governing docs:** cumulative-drift-governance §2–3 (schema verbatim; concrete numbers = Blocker B7, ship conservative placeholders flagged `placeholder: true`).
- **Steps:** budget engine tracking per 30d window: promotions count, files/configs/prompts changed, routing-weight movement, aggregate behavioural delta (eval-suite score drift), cost delta, quality delta, rollback chain depth, time since human checkpoint; freeze on any breach (budget exceeded, unexplained quality regression, cost spike, security-class change, chain depth) — improvement + L0 lanes halt until `unfreeze` (audited, R-level per profile); digest v2: budget consumption bars, before/after metrics per change, rollback links, placeholder-numbers banner until B7 resolved.
- **Outputs:** `internal/evolve/budget.go`; digest v2; freeze e2e (each breach type).
- **Acceptance:** 5/5 breach types freeze; unfreeze audited; digest renders fixture window correctly.
- **Validation:** `go test ./internal/evolve/ -run Budget && bash test/freeze_matrix_e2e.sh`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 76 (EVO-03) [P] — Memory curator with provenance

- **Goal:** Durable, curated, deletable memory: evidence in, provenance-stamped knowledge out.
- **Depends:** 66 · **Governing docs:** `docs/foundry/docs/architecture/data-consistency.md` (memory sections).
- **Steps:** memory rows {content, kind, provenance evidence refs, profile scope, confidence, ttl}; curator job: evidence → candidate memories (LLM, cassette-tested) → dedupe/merge → store; retrieval API scoped per profile (cross-profile read impossible — test); deletion cascades honored (Task 66 integration); vector index optional behind interface (pgvector) with delete-with-source proof.
- **Outputs:** `internal/memory/*`; migration `0016_memory.sql`; cascade + isolation tests.
- **Acceptance:** isolation test green; deleting source evidence deletes derived memory + vectors.
- **Validation:** `go test ./internal/memory/... -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 77 (EVO-04) — Capability evolution loop (bounded L1)

- **Goal:** Prompt/skill changes (L1) through generate→evaluate→quarantine→shadow→canary→promote, personal profile only.
- **Depends:** 75 · **Governing docs:** capability-evolution workflow; drift-governance L1 conditions (no new permissions, no new data class, no budget increase, reversible, previous retained).
- **Steps:** skill/prompt registry with versions + eval suites per skill (golden tasks scored deterministically); evolution workflow honoring quarantine (never on critical path until shadow-clean) and L1 condition checks as deterministic gates; promotion = registry version bump inside drift budget; org profiles: proposal-only (H); e2e: a prompt improvement flows to promotion; a permission-expanding candidate is rejected at the L1 gate.
- **Outputs:** `internal/evolve/skills.go`; eval harness; both-path e2e.
- **Acceptance:** condition-gate matrix green; org proposal-only proven.
- **Validation:** `bash test/skill_evolution_e2e.sh`.
- **Risk:** High · **Exec:** go-kernel+integration · **Rev:** **R3** · **Status:** ☐ Not started

### Task 78 (EVO-05) [P] — Multi-repository 10x change-set saga

- **Goal:** One initiative, N repos: contract freeze, parallel isolated tasks, ordered integrations, cross-repo receipts.
- **Depends:** 63 · **Governing docs:** `docs/foundry/docs/workflows/multi-repository.md`; ten-x-branch.
- **Steps:** change-set saga workflow: freeze declared cross-repo contracts (interface digests) → per-repo TenXDeliver children (parallel) → ordered integration per dependency graph → all-or-honest-partial semantics: failed repo ⇒ pushed repos recorded, saga ends `PROVEN_BLOCKED` with per-repo receipt map + next_action (no automatic revert of shared branches — humans own that; documented); environment revision provenance recorded; e2e on 3 fixture repos incl. one seeded failure.
- **Outputs:** `internal/kernel/changeset.go`; saga e2e.
- **Acceptance:** parallel children isolated; ordered integration respected; partial-failure receipt map exact.
- **Validation:** `bash test/changeset_e2e.sh`.
- **Risk:** High · **Exec:** go-kernel · **Rev:** **R3** · **Status:** ☐ Not started

### Task 79 (EVO-06) [P] — OpenAI + local-model providers

- **Goal:** Provider breadth through the execution-class contract; no capability assumptions.
- **Depends:** 34 · **Governing docs:** `docs/foundry/docs/providers/{openai.md,local-models.md,provider-execution-classes.md}` (verify current APIs at implementation).
- **Steps:** OpenAI executor adapter (API class) + local (OpenAI-compatible endpoint, e.g. Ollama) behind the Task 10 contract; capability declaration files per provider (no implicit features); routing policy hook (model per task class from config); cost metering with pricing_version per call (local = zero + optional shadow); contract test suite runs per provider (gated).
- **Outputs:** `internal/executor/{openai,local}/`; capability files; routing config; gated contract tests.
- **Acceptance:** shared executor contract suite green per provider; classification policy respected (customer data never to providers without grant — test).
- **Validation:** `go test ./internal/executor/... -race` (+ gated live).
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Status:** ☐ Not started

### Task 80 (EVO-07) [P] — Figma API mockup ingestion

- **Goal:** Upgrade Task 43 with structured Figma input (nodes, components, prototypes) — richer Observed extraction.
- **Depends:** 43 · **Steps:** Figma REST ingestion (file → node tree, component set, prototype flows) mapped into Task 43 stage structs; Observed label now allowed for structurally-present facts (component exists, flow edge exists) — inference stages unchanged; token via secrets, read-only scope; fixture file + recorded API cassette; a11y extraction from Figma metadata where present.
- **Outputs:** `internal/spec/mockup/figma.go`; cassettes; label-strength tests updated.
- **Acceptance:** same spec pipeline output shape; Figma-sourced Observed items carry node refs as Basis.
- **Validation:** `go test ./internal/spec/mockup/ -run Figma`.
- **Risk:** Med · **Exec:** integration · **Rev:** R2 · **Status:** ☐ Not started

### Task 81 (EVO-08) — Portfolio scaling: multi-mission scheduler

- **Goal:** N missions, fair and budget-isolated, per `maximum_active_products` and portfolio loop contract.
- **Depends:** 53 · **Steps:** portfolio workflow supervising mission workflows; per-mission budget isolation (Task 29 scopes already support); fairness across missions (Task 65 quotas extended); portfolio-level decide records (invest more / hold / kill-candidate proposals — H for kill of revenue-bearing product per touchpoint inventory); portfolio dashboard panel + digest section.
- **Outputs:** `internal/mission/portfolio.go`; quota extension; tests (2-mission fixture, one starving attempt).
- **Acceptance:** budget bleed between missions impossible (ledger proof); fairness bound holds.
- **Validation:** `go test ./internal/mission/ -run Portfolio -race`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 82 (EVO-09) [P] — Capacity-aware learning integration

- **Goal:** Learning consumes leftover capacity only; never competes with delivery.
- **Depends:** 74 · **Governing docs:** `docs/foundry/docs/operations/capacity.md` (capacity-aware self-learning).
- **Steps:** capacity signal source (provider budgets, worker saturation from Task 31) → learning lane scheduler admits eval/shadow work only above headroom threshold; brownout integration (first shed — already Task 33; verify end-to-end); starvation-of-learning acceptable, starvation-of-delivery impossible (test under load).
- **Outputs:** `internal/evolve/capacity.go`; load test.
- **Acceptance:** delivery p95 unchanged (±5%) with learning lane saturated.
- **Validation:** `make soak-learning`.
- **Risk:** Med · **Exec:** go-backend · **Rev:** R2 · **Status:** ☐ Not started

### Task 83 (EVO-10) — BillingMaturity graduation → bounded A2 billing (C19)

- **Goal:** The last gate: billing changes graduate from Tier H to bounded A2 only on proven evidence.
- **Depends:** 69 · **Governing docs:** admission-tiers §3 (graduation evidence verbatim); Blocker B6 (bounds = owner decision; conservative defaults shipped).
- **Steps:** BillingMaturity evaluator over real ledger/incident data (3 cycles, 10 charges, 0 unresolved incidents/chargebacks, refund rate, test suite, idempotency proof, recovery test record); graduation = signed record (R4 human sign-off first time); post-graduation classifier rules: bounded non-destructive implementation changes → A2 with the money-semantic list (amount, currency, tax, refund, renewal, cancellation, proration, trial, migration, provider, payment-data) hard-pinned H unless mission pre-authorization exists; regression: any billing incident post-graduation auto-revokes maturity (back to H) + P1.
- **Outputs:** `internal/billing/maturity.go`; classifier v1.2 rules + goldens; revocation path test.
- **Acceptance:** graduation impossible on fixture missing any criterion; money-semantic list provably H post-graduation; incident revokes.
- **Validation:** `go test ./internal/billing/ -run Maturity && go test ./internal/admission/... -run Golden -count=5`.
- **Risk:** **High** · **Exec:** go-kernel · **Rev:** **R4** (first graduation) · **Status:** ☐ Not started

---

## K. Execution waves (global)

### D-P6 — Execution waves

```mermaid
flowchart TD
    subgraph M0[Weeks 1-4: M0]
        S0[Wave S0: Tasks 1-6] --> S1[Wave S1: Tasks 7-11] --> S2[Wave S2: Tasks 12-15] --> S3[Wave S3: Tasks 16-19]
    end
    subgraph M1[Weeks 5-12: M1]
        F0[Wave F0: Tasks 20-23] --> F1[Wave F1: Tasks 24-28] --> F2[Wave F2: Tasks 29-34] --> F3[Wave F3: Tasks 35-39]
    end
    subgraph PT[Parallel tracks]
        A1[A1: 40-45] --> A2[A2: 46-49] --> A3[A3: 50-53]
        B1[B1: 54-56] --> B2[B2: 57-59] --> B3[B3: 60-63]
    end
    S3 --> F0
    F3 --> A1
    F3 --> B1
    A3 --> M2W[M2: 64-73]
    B3 --> M2W
    M2W --> M3W[M3: 74-83]
```

## L. Critical path

```text
Task 1 → 2 → 3 → 6 → 7 → 8 → 12 → 16 → 19 (M0 exit)
→ 20 → 21 → 22 → 24 → 25 → 27 → 29 → 32 → 39 (M1 exit)
→ Track A long pole: 40 → 45 → 47 → 51 → 53
   (Track B: 55 → 58 → 61 → 63 — finishes earlier; pull 64/67/68 into its slack)
→ 64 → 70 → 73 (M2 exit) → 74 → 75 → 77
```

## M. Parallel work map

- `[P]` tasks within a wave: disjoint packages, safe concurrently (see Index column).
- Cross-track after Task 39: 40–53 and 54–63 share only M1 interfaces — interleave freely (solo rhythm: Track A first half of week, Track B second) or run per-agent; under orchestrator-driven execution (§A), the runner may simply dispatch both tracks' next-eligible tasks concurrently up to its concurrency cap.
- **Never parallelize:** 7 with 8 (admission↔provenance contract churn) · 27 with 58 (push-authority surface) · any two tasks adding files under `migrations/` (serialize by number reservation in PR title) · 22 with 54 (policy schema churn).
- Long-running gated live tests (17, 47, 49, 62, 63) run off the critical path on separate branches.

## N. First 30-day execution plan

| Days  | Tasks                          | Checkpoint                                                                                                      |
| ----- | ------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| 1–4   | 1, 2, then 3 (runner)          | CI green; harness complete; **runner live — orchestrator drives from here, you only answer its Telegram gates** |
| 5–9   | 4, 5, 6 `[P]`                  | compose services up; state package R3-approved; plans parse with stable digests                                 |
| 10–14 | 7 → 8; 9+10+11 `[P]` alongside | signed ApprovedPlan e2e; golden admission corpus                                                                |
| 15–19 | 12, 13                         | hello-world + failing plans reach correct terminals on Temporal                                                 |
| 20–23 | 14+15 `[P]`, 18                | projection rebuild proof; fitness suite biting                                                                  |
| 24–27 | 16 (+17 flagged)               | **resume proof 20/20 green** — High/R3, so even in orchestrator mode this one waits for your Telegram approval  |
| 28–30 | 19; open 20–21                 | M0 exit report + tag; foundation started                                                                        |

**Day-27 tripwire:** resume proof not green ⇒ stop, replan (do not push through).

## O. Definition of Done

**Task-level:** outputs exist at stated paths · acceptance criteria demonstrably met · validation commands green in CI · evidence archived to `evidence/task-<N>/` · reviewer at stated R-level approved (deterministic checks override LLM opinion) · no constitution article violated · boundary respected · Status flipped + Index box checked.
**Milestone-level:** exit criteria + Constitution Check + evidence archive green (Tasks 19, 39, 53, 63, 73).
**System-level:** _first value_ = Tasks 1–39 + either track exit; _plan-complete_ = Task 73 with Tasks 74–83 running as governed autonomy.

## P. Implementation risks (top 5)

1. **Scope gravity** — the docs describe a platform; the cards ship slices. Out-of-scope lists are contractual; reviewers reject extras even when tests pass.
2. **Temporal misuse** — replay tests mandatory on every kernel change (CI path filter); no wall-clock/rand in workflow code (linted).
3. **Classifier false-negatives** — discrepancy-raises-tier rule, growing golden corpus, H-tier fixtures in every e2e, incident→fixture pipeline.
4. **Solo-builder stall** — day-27 and per-milestone tripwires are stop-and-replan triggers, not vanity gates; M3 requires a replan at entry by design.
5. **Provider/API drift** — Telegram/Stripe/Fly/Claude-Code/Figma details verified at implementation time per provider-doc staleness rule; adapter contract tests isolate blast radius.

## Q. Deferred capabilities

L2–L4 auto-promotion (human-gated standing rule) · org-wide skill promotion · cross-profile memory · real-money billing pre-maturity · marketplace/opportunity discovery automation · deep Jira/Confluence/TestRail adapters · Backstage-style catalog UI · advanced memory beyond curator v1 · Temporal Cloud migration (B3) · Vault/KMS backend (B4).

**Noted from the loop-engineering comparison against Claude Code's own loop taxonomy (`claude.com/blog/getting-started-with-loops`) — deliberately deferred until after Task 83, not dropped:**

- **Cost-tiered model routing as its own explicit practice** — route mundane/repetitive loop steps to smaller/faster models, reserve the most capable model for judgment calls (the blog names this explicitly for proactive loops). Foundry's routing infrastructure (`docs/foundry/docs/providers/provider-execution-classes.md`, routing weights referenced in Task 74) already supports this; it just isn't yet _specified_ as its own enforced tiering rule. Natural home: extend Task 30 (Foundation) or Task 79 (EVO-06 provider breadth), once real usage data exists to tier against.
- **Plain PLAN.md → PR path** — the simplest general-purpose workflow (no venture specifics, no 10x-branch specifics; `docs/foundry/docs/workflows/direct-plan.md`'s N17.1), sitting below both Track A and Track B in complexity. Everything underneath it already exists once the Foundation ships (worktree, build, verify, evidence, push — Tasks 1–39); only the PR-opening step is missing. Natural home: a small extension to Task 27 (FND-08, GitHub SCM adapter) rather than a new numbered task.

Rationale for deferring both: neither blocks Tasks 1–83, and adding either now risks the exact accretion pattern the original V11→V12 review flagged — new capability bolted on before the foundation it depends on is proven. Revisit both once the Shared Kernel Proof (Task 19 exit) and Foundation (Task 39 exit) are real, not hypothetical.

## R. Planning blockers (owner decisions — defaults let work proceed)

| ID  | Decision                                                         | Default in force                                                                                                                             |
| --- | ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | Personal deploy target allowlist                                 | Fly.io behind `deploy.Adapter` seam (Task 47)                                                                                                |
| B2  | Track B SCM order (Company = Bitbucket)                          | GitHub first; Task 62 adds Bitbucket before live org use                                                                                     |
| B3  | Temporal self-hosted vs Cloud at prod                            | dev server → self-hosted compose (Task 71); revisit M2                                                                                       |
| B4  | Secrets backend                                                  | age-file behind interface (Task 35); Vault/KMS at M2+                                                                                        |
| B5  | IdP for OIDC/WebAuthn                                            | managed IdP, Zitadel-class — **decide before Task 25**                                                                                       |
| B6  | Post-maturity billing A2 bounds                                  | all money-semantics stay H (Task 83 defaults)                                                                                                |
| B7  | Concrete CumulativeChangeBudget numbers                          | conservative placeholders flagged in Task 75; set from first 2 weeks of promotion data                                                       |
| B8  | Claude Code automated-use constraints (ToS/limits)               | verify before Task 17 runs unattended; fake executor unblocks everything meanwhile                                                           |
| B9  | ~~Nested containers for Task 34's sandbox tests~~ — **resolved** | Hybrid: bare-runner CI lane is authoritative (gates merges); local `dev`-via-socket-mount lane is convenience-only, non-gating (see Task 34) |

No normative V12 contradictions remain; `docs/foundry/docs/legacy/**` contributed zero guidance to this plan.

## S. Recommended first task

**Task 1.** Dependency-free, creates the deterministic entry points (`make bootstrap|test|fitness`) every subsequent task and every agent session relies on, and unlocks Task 2 plus Tasks 4–6 in parallel the moment CI is green (Task 3, the autonomous runner, follows immediately after Task 2's harness lands — after that, it drives). Run Task 1 manually: `Implement Task 1 from docs/PLAN.md` with your `implement-and-review-task.md` prompt.

---

## T. Completion log

Agents append one line per completed task: `Task <N> — ✅ <date> — <commit> — <evidence path>`

```text
(empty — starts with Task 1)
```
