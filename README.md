# Delivery Foundry

> A governed control plane for loop-engineered software delivery — AI agents execute under explicit policy, budgets, and evidence, not implicit trust.

**Topics:** `ai-agents` · `temporal` · `golang` · `orchestration` · `autonomy` · `governance` · `docker` · `evidence` · `policy` · `venture` · `10x-engineering`

**Delivery Foundry** is the durable runtime that turns an idea, mockup, or approved plan into verified delivery work. A shared **kernel** owns sequencing, side effects, budgets, and evidence. A **PEC** (Plan Execution Coordinator) only proposes waves — it never decides. Two product tracks share that kernel:

| Track                               | Profile                       | What you get                                                                                          |
| ----------------------------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------- |
| **A — Personal Autonomous Venture** | `personal-autonomous-venture` | Idea/mockup/plan → mission → sandboxed delivery → deploy → observe → improve (or roll back)           |
| **B — Organization / 10x**          | `organization-10x`            | Approved plan → concurrent tasks → atomic group → CAS branch handoff (**no** PR/merge/staging/deploy) |

The golden rule: **evidence-based completion**. Nothing is “done” because an agent said so — manifests, digests, budgets, and independent verification decide.

### Loop engineering

Foundry is not a one-shot codegen pipeline. It runs **nested loops** that share evidence but not authority — the kernel sequences side effects; the PEC only proposes; security and budgets constrain every hop.

```mermaid
flowchart TB
  subgraph Outer["Portfolio / mission loop"]
    INTENT["IDEA · MOCKUP · PLAN · mission"]
    INVEST["Choose what deserves investment<br/>budget · profile · readiness"]
  end

  subgraph Delivery["Delivery loop"]
    ADMIT["Deterministic admission<br/>A0 / A1 / A2 / H"]
    BUILD["Sandbox build"]
    REVIEW["Independent review"]
    VERIFY["Evidence verify"]
    SHIP["Ship<br/>personal deploy · or · 10x branch handoff"]
  end

  subgraph Feedback["Observe → improve"]
    OBSERVE["Observe<br/>activation · billing · drift · CI"]
    IMPROVE["Bounded improvement plan<br/>still re-admitted"]
    DECIDE{"Retain · roll back · pause · kill"}
  end

  subgraph Cross["Cross-cutting loops"]
    RECOVERY["Recovery<br/>checkpoint · retry · reroute · rollback"]
    CAPACITY["Capacity<br/>reserve · wait · fail over"]
    CAPABILITY["Capability<br/>discover · evaluate · canary"]
    LEARNING["Learning<br/>propose from evidence"]
    MEMORY["Memory<br/>promote trusted knowledge"]
    SECURITY["Security<br/>authorize · audit · contain · revoke"]
  end

  INTENT --> INVEST --> ADMIT
  ADMIT --> BUILD --> REVIEW --> VERIFY --> SHIP
  SHIP --> OBSERVE --> DECIDE
  DECIDE -->|"improve"| IMPROVE --> ADMIT
  DECIDE -->|"retain"| OBSERVE
  DECIDE -->|"roll back"| RECOVERY
  DECIDE -->|"pause / kill"| INTENT

  RECOVERY <--> Delivery
  CAPACITY <--> Delivery
  CAPABILITY --> LEARNING --> MEMORY --> ADMIT

  SECURITY -. constrains .-> Outer
  SECURITY -. constrains .-> Delivery
  SECURITY -. constrains .-> Feedback
  SECURITY -. constrains .-> Cross
```

In practice, Track A closes the product loop (`MissionLoop` → `DeliverPlan` → deploy → observe → `ImprovementLoop` → re-admit). Track B stops at verified branch handoff — no PR, merge, or deploy.

---

## How it works

```mermaid
flowchart LR
  subgraph Inputs
    IDEA["IDEA / MOCKUP / PLAN"]
  end

  subgraph ControlPlane["Control plane"]
    CLI["foundry CLI"]
    DAEMON["foundryd"]
    ROUTER["Input router"]
    KERNEL["Kernel"]
    PEC["PEC proposes only"]
  end

  subgraph Durable
    PG["PostgreSQL"]
    TEMP["Temporal"]
    EV["Evidence store"]
  end

  subgraph Effects["Kernel-owned side effects"]
    SBX["Sandbox executor"]
    SCM["SCM write / CAS push"]
    DEP["Personal deploy"]
  end

  IDEA --> CLI
  CLI --> ROUTER
  ROUTER --> DAEMON
  DAEMON --> KERNEL
  PEC -.->|"proposals"| KERNEL
  KERNEL --> TEMP
  KERNEL --> PG
  KERNEL --> EV
  KERNEL --> SBX
  KERNEL --> SCM
  KERNEL --> DEP
```

Every `make <target>` runs inside the `dev` Docker image. Host requirements stay at **Docker + Compose + GNU make** — no local Go, Node, or Playwright install.

---

## Scenarios

Use this section when you know _what you have_ (idea, plan, mockup, repo, or nothing) and need the matching Foundry path.

In the examples below:

```sh
# Shorthand used only in this README — expand to the real docker compose form:
FOUNDRY='docker compose -f deploy/docker-compose.yaml run --rm dev go run ./cmd/foundry'
FOUNDRYD='docker compose -f deploy/docker-compose.yaml run --rm --service-ports dev go run ./cmd/foundryd'
```

### Choose your path

| You have…                                               | You want…                           | Track    | Start with                                                                              |
| ------------------------------------------------------- | ----------------------------------- | -------- | --------------------------------------------------------------------------------------- |
| A raw product idea only                                 | Research → spec → PLAN → mission    | Personal | [1. From idea](#1-build-from-an-idea--what-do-i-provide-first)                          |
| An executable `PLAN.md` + an existing git repo          | Deliver that plan (skip ideation)   | Personal | [2. From plan + existing repo](#2-build-from-a-plan--repo-folder-already-exists)        |
| An idea or plan, but **no** product repo yet            | Scaffold a product, then deliver    | Personal | [3. No project folder yet](#3-build-something--no-project-or-repo-folder-yet)           |
| An existing product repo that already contains a plan   | Improve / deliver against that plan | Personal | [4. Improve an existing repo](#4-improve-an-existing-project--plan-already-in-the-repo) |
| UI mockups (image / PDF / HTML / Figma) under a product | Spec → PLAN → mission/delivery      | Personal | [5. From mockup](#5-build-from-a-mockup-under-a-project-repo)                           |
| Telegram chat access                                    | Same intake as CLI, after confirm   | Personal | [6. From Telegram](#6-start-from-telegram-idea-or-attachment)                           |
| An **approved** organization PLAN                       | Concurrent tasks → branch handoff   | **10x**  | [7. Organization / 10x](#7-organization--10x-handoff-from-an-approved-plan)             |
| Org mockup (not personal deploy)                        | Labeled spec → org PLAN → 10x       | **10x**  | [8. Organization mockup](#8-organization-mockup--then-10x)                              |
| Approved plan, one-shot delivery (no long mission loop) | `DeliverPlan` only                  | Personal | [9. Direct plan delivery](#9-direct-plan-delivery-no-mission-loop)                      |
| A paused / failed intake or mission                     | Resume without repeating stages     | Either   | [10. Resume / operate](#10-resume-operate-pause-kill-raise-budget)                      |

### Shared prerequisites (almost every live scenario)

Do these once before live workflows:

```sh
cp .env.example .env
make bootstrap
make up
make doctor
make migrate-up

# Approver keys (plan approve + production intake)
$FOUNDRY keygen

# Daemon with Temporal workers (needed for mission / deliver / 10x)
$FOUNDRYD
```

Offline experiments can use `--dry-run` and cassette/fixture flags without a live daemon.

---

### 1. Build from an idea — what do I provide first?

**Provide first (required):**

| Input                               | Why                                                      |
| ----------------------------------- | -------------------------------------------------------- |
| Idea text                           | Untrusted intent — never treated as a system instruction |
| Budget envelope (`--budget`)        | Caps research + MVP spend **before** any spend (C19)     |
| Product repo URL (`--repo-url`)     | Where delivery writes (least-privilege path)             |
| Write scope (`--repo-write-target`) | Never `*` — e.g. `src/` or `apps/web/`                   |
| Profile / principal                 | Personal venture profile + who is acting                 |

**Also needed for a real BUILD (production path):**

- Postgres + Temporal (`make up`)
- Approver keys (`foundry keygen`)
- An allowlisted **real validation signal** for the opportunity (synthetic/test signals refuse BUILD)
- Spec synthesis source (`FOUNDRY_SPEC_CASSETTE` or a wired live CandidateSource)
- Running `foundryd`

**Optional / offline:**

- `--opportunity-fixtures` + `--spec-cassette` for deterministic dry runs
- `--dry-run` (in-memory recording starter — no Temporal)

```sh
# Offline rehearsal (no daemon required)
$FOUNDRY mission start \
  --idea "Build a SaaS for engineering managers that solves X" \
  --budget 50 \
  --repo-url https://github.com/you/product.git \
  --repo-write-target src/ \
  --opportunity-fixtures test/fixtures/opportunity \
  --spec-cassette test/cassettes/spec/idea-scheduling.json \
  --dry-run

# Production path: omit fixture flags; require Temporal + keys + real signal
$FOUNDRY mission start \
  --idea "Build a SaaS for engineering managers that solves X" \
  --budget 50 \
  --repo-url https://github.com/you/product.git \
  --repo-write-target src/
```

**Possible outcomes (all are successful terminals when designed that way):**

| Verdict / stage           | Meaning                         | Next action                                      |
| ------------------------- | ------------------------------- | ------------------------------------------------ |
| `BUILD` → mission started | Signal + admission passed       | Watch `mission status` / Telegram                |
| `VALIDATE-MORE`           | Missing real signal or evidence | Collect allowlisted signal, `intake resume`      |
| `REJECT`                  | Opportunity refused             | No repo/budget/deploy was created — reframe idea |
| `AWAITING_STRONG_AUTH`    | H-tier plan                     | Complete OIDC+WebAuthn outside Telegram          |
| `AWAITING_READINESS`      | Mission ceremony incomplete     | Answer ceremony / readiness gaps                 |

Inspect runs:

```sh
$FOUNDRY intake list
$FOUNDRY intake show <run-id>
$FOUNDRY opportunity list
```

---

### 2. Build from a plan — repo folder already exists

You already wrote (or generated) an executable `PLAN.md` and have a git remote ready.

**Provide first:**

| Input                                                      | Why                          |
| ---------------------------------------------------------- | ---------------------------- |
| `PLAN.md` path                                             | Executable plan artifact     |
| Repo URL + revision (org path) or personal profile binding | Provenance / source pin      |
| Approver identity                                          | `foundry keygen` + principal |
| Running `foundryd`                                         | Kernel starts `DeliverPlan`  |

```sh
# 1) Parse / submit
$FOUNDRY plan submit ./path/to/PLAN.md

# Organization provenance (optional flags when submitting as org work)
$FOUNDRY plan submit --org --repo https://bitbucket.org/org/repo.git --rev <sha> ./PLAN.md

# 2) Approve (signs ApprovedPlan; H-tier needs explicit ack / strong auth)
$FOUNDRY plan approve ./path/to/PLAN.md

# 3) Deliver through the API (kernel resolves lane/executor — not the CLI)
$FOUNDRY plan run --plan-id <approved-plan-id>
```

This path **skips ideation and opportunity research**. Admission, sandbox, evidence, and budgets still apply.

---

### 3. Build something — no project or repo folder yet

Foundry does not invent a product repository for you on the personal path without an explicit product/repo step.

**Provide first:**

1. Product name
2. Where to materialize the template
3. Then push that folder to a remote and use scenarios 1, 2, or 5

```sh
# Scaffold from the product template
$FOUNDRY product new --from-template -name my-saas --out ./products

# Create the remote yourself (gh, git, Bitbucket UI, …), then:
cd ./products/my-saas
git init && git add . && git commit -m "chore: scaffold from foundry template"
git remote add origin https://github.com/you/my-saas.git
git push -u origin main

# Now attach Foundry to that remote
$FOUNDRY mission start \
  --idea "…" \
  --budget 50 \
  --repo-url https://github.com/you/my-saas.git \
  --repo-write-target src/
```

If you already have a PLAN for the new product, after the first push use [scenario 2](#2-build-from-a-plan--repo-folder-already-exists) (`plan submit` → `approve` → `plan run`).

---

### 4. Improve an existing project — plan already in the repo

Two common shapes:

**A. One-shot delivery of the in-repo plan** (no long-running mission):

```sh
# From a checkout that contains docs/PLAN.md (or similar)
$FOUNDRY plan submit ./docs/PLAN.md
$FOUNDRY plan approve ./docs/PLAN.md
$FOUNDRY plan run --plan-id <id>
```

**B. Long-running venture loop** (observe → improve → redeploy / roll back):

```sh
# Mission already exists and is READY
$FOUNDRY mission start <mission-id>
$FOUNDRY mission status <mission-id>

# Bounded autonomous improvement is driven by MissionLoop → ImprovementLoop
# when observation says "improve". Freeze blocks the next cycle:
$FOUNDRY promotions unfreeze --product <product-id>   # only after durable correction
```

**Provide first:** plan path or mission id, repo write scope, budget envelope still open, profile that allows the improvement tiers.

---

### 5. Build from a mockup under a project repo

Mockup is a **first-class entry** (C16). Pixels are never silent requirements — every extracted item is `Observed` / `Inferred` / `Assumed` / `Unresolved`.

**Provide first:**

| Input                                  | Why                                 |
| -------------------------------------- | ----------------------------------- |
| Mockup file(s) or Figma/HTML/PDF/image | Original bytes preserved + digested |
| Product repo URL / write target        | Where the derived PLAN will deliver |
| Budget                                 | Same fail-closed envelope rules     |
| Cassette or vision provider            | Offline vs live extraction          |

```sh
# Extract labeled requirements → spec or PLAN
$FOUNDRY mockup extract \
  --input ./designs/checkout.png \
  --out ./out/mockup-spec.json

$FOUNDRY mockup extract \
  --input ./designs/flows.pdf \
  --out ./out/PLAN.md

# Then admit/approve/deliver like any other plan
$FOUNDRY plan submit ./out/PLAN.md
$FOUNDRY plan approve ./out/PLAN.md
$FOUNDRY plan run --plan-id <id>
```

High-impact `Unresolved` items raise admission risk and may pause for human gate — they do not silently become product behavior.

Telegram attachments (image/PDF) follow the same mockup semantics after `/confirm` ([scenario 6](#6-start-from-telegram-idea-or-attachment)).

---

### 6. Start from Telegram (idea or attachment)

Telegram is transport only (C11): drafts + low-risk commands. It never grants high-risk approval.

**Provide first:** registered chat→principal binding, bot token (`FOUNDRY_TELEGRAM_BOT_TOKEN`), running `foundryd`.

```text
/idea Build a SaaS for X, budget $50
→ Foundry replies with a normalized summary + one-time nonce
/confirm <nonce>
→ Shared production intake (same path as CLI IDEA)

# Attachments create MOCKUP drafts (bytes preserved), then:
/confirm <nonce>

# Ops (low-risk)
/status
/pause
/resume
```

H-tier approval uses a short-lived OIDC+WebAuthn URL — **not** `/approve` inside Telegram.

---

### 7. Organization / 10x handoff from an approved plan

10x terminal success is **only** `SUCCEEDED` / `TEN_X_BRANCH_HANDOFF_READY`. No PR, merge, staging, or deploy (C15).

**Provide first:**

| Input                            | Why                                  |
| -------------------------------- | ------------------------------------ |
| Signed `ApprovedPlan`            | Immutable authority artifact         |
| Execution envelope digest        | Kernel-resolved scope (Task 141)     |
| Organization profile + SCM token | Bitbucket/GitHub write identity      |
| Disposable / handoff remote      | Never production org repo for proofs |

```sh
$FOUNDRY plan submit --org --repo https://bitbucket.org/org/repo.git --rev <sha> ./PLAN.md
$FOUNDRY plan approve ./PLAN.md
# Production 10x start rejects caller-supplied change sets — plan+envelope only.
# Drive via foundryd / API once TenX orchestration is configured for the profile.
```

Caller-supplied change sets are **refused**. PEC proposes waves; the kernel validates, sandboxes, verifies, and CAS-pushes.

---

### 8. Organization mockup → then 10x

Same mockup extraction as personal, but route mode is **organization**: labeled spec/PLAN with strong provenance, then scenario 7. Personal market/deploy semantics must not run on this path.

Unsupported: free-form organization IDEA unless a governed policy route exists (input router refuses otherwise).

---

### 9. Direct plan delivery (no mission loop)

When you want a single `DeliverPlan` execution without MissionLoop cadence:

```sh
$FOUNDRY plan submit ./PLAN.md
$FOUNDRY plan approve ./PLAN.md
$FOUNDRY plan run --plan-id <id>          # optional: --lane <queue-lane>
$FOUNDRY evidence verify <bundle-id>
```

Use a mission ([scenarios 1 / 4](#1-build-from-an-idea--what-do-i-provide-first)) when you need observe → improve → redeploy over time.

---

### 10. Resume, operate, pause, kill, raise budget

```sh
$FOUNDRY intake resume <run-id>     # continue from last durable stage (VALIDATE-MORE, etc.)
$FOUNDRY mission list
$FOUNDRY mission status <id>
$FOUNDRY mission pause <id>
$FOUNDRY mission resume <id>
$FOUNDRY mission kill <id>          # clean CANCELLED / MISSION_KILLED

# Mid-mission budget ceiling raise (C19 — explicit, audited; never silent)
$FOUNDRY budget raise \
  --scope mission:<mission-id> \
  --kind experiment \
  --period 2026-08 \
  --ceiling 100 \
  --raised-by <principal> \
  --reason "extend MVP validation" \
  --workflow-id <paused-workflow-id>   # optional: resume a paused DeliverPlan
```

Duplicate starts with the same idempotency key must not create a second mission.

---

### 11. Bootstrap the control plane (first time on a machine)

```sh
cp .env.example .env
make bootstrap
make up
make doctor
make test lint fitness
```

---

### 12. Quality gates, evidence, and driving this repo’s own PLAN

```sh
make evidence-verify
make fitness
make doclint    # docs, .ai/, compose, Docker, workflows

# Autonomous runner for docs/PLAN.md (Foundry building itself)
# Optional: TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID for High/R3–R4 gates
make plan-run
```

---

## Installation

## Installation

### What you need

| Tool                     | Version / notes |
| ------------------------ | --------------- |
| Docker Engine or Desktop | latest          |
| Docker Compose           | v2              |
| GNU Make                 | 3.81+           |
| Git                      | any recent      |

No local Go, Node, Playwright, or Postgres install is required. Those live inside the `dev` image (`deploy/Dockerfile.dev`).

### Clone and configure

```sh
git clone https://github.com/okfriansyah-moh/the-foundry.git
cd the-foundry
cp .env.example .env
```

Edit `.env` only as needed. Defaults already point `dev` at compose services:

| Variable                                  | Purpose                                                                           |
| ----------------------------------------- | --------------------------------------------------------------------------------- |
| `PG_DSN`                                  | Postgres DSN (`postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable`) |
| `TEMPORAL_HOSTPORT`                       | Temporal frontend (`temporal:7233`)                                               |
| `FOUNDRY_DATA_DIR`                        | Local data dir bind-mounted into `dev`                                            |
| `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` | Optional plan-runner bootstrap bot                                                |
| `FOUNDRY_TELEGRAM_BOT_TOKEN`              | Production foundryd bot (separate from plan-runner)                               |
| `FOUNDRY_OIDC_*` / `FOUNDRY_WEBAUTHN_*`   | Strong-auth IdP (required for H-tier / production strong auth)                    |
| `GITHUB_TOKEN` / `BITBUCKET_API_TOKEN`    | Kernel SCM write tokens (least privilege; never for sandbox)                      |

### Build the toolchain and start services

```sh
make bootstrap   # build `dev` image + go mod download
make up          # postgres + temporal on the compose network
make doctor      # Docker health + PG SELECT 1 + Temporal GetSystemInfo
```

| Service    | Reachability                                           |
| ---------- | ------------------------------------------------------ |
| `dev`      | Ephemeral via `make` / `docker compose run --rm dev …` |
| `postgres` | Internal only (`postgres:5432`)                        |
| `temporal` | Internal only (`temporal:7233`)                        |

Optional observability profile:

```sh
make up PROFILE=obs   # also starts prometheus + grafana
```

Stop services:

```sh
make down             # stop and remove volumes
make down KEEP_DATA=1 # keep the postgres volume
```

---

## Quick Start

```sh
# 1. Clone + env
git clone https://github.com/okfriansyah-moh/the-foundry.git
cd the-foundry
cp .env.example .env

# 2. Toolchain + runtime deps
make bootstrap
make up
make doctor

# 3. Quality gate (same shape as CI `build`)
make test lint fitness

# 4. Apply migrations (also covered by CI `migrations` job)
make migrate-up

# 5. Generate local approver keys (for plan approve / production intake)
docker compose -f deploy/docker-compose.yaml run --rm dev \
  go run ./cmd/foundry keygen

# 6. Run the daemon (Temporal workers + API) when you need live workflows
docker compose -f deploy/docker-compose.yaml run --rm --service-ports dev \
  go run ./cmd/foundryd
```

Then use `go run ./cmd/foundry …` (always through `dev`) for CLI operations.

---

## Commands

All day-to-day operations go through the Makefile / `dev` container. Prefer `make <target>` when one exists.

### Make targets

| Command                                               | Description                                                   |
| ----------------------------------------------------- | ------------------------------------------------------------- |
| `make bootstrap`                                      | Build `dev` image and download Go modules                     |
| `make up` / `make down`                               | Start/stop postgres + temporal                                |
| `make doctor`                                         | Host Docker check + PG/Temporal connectivity                  |
| `make test`                                           | `go test ./...` inside `dev`                                  |
| `make lint`                                           | `golangci-lint` inside `dev`                                  |
| `make fitness`                                        | Constitution / boundary / enum fitness suite                  |
| `make doclint`                                        | Docs + harness reproducibility lints                          |
| `make migrate-up` / `migrate-down` / `migrate-status` | Goose migrations                                              |
| `make evidence-verify`                                | Verify evidence markers / bundles                             |
| `make plan-run`                                       | Autonomous `docs/PLAN.md` runner                              |
| `make e2e-venture` / `e2e-tenx` / `e2e-github`        | Gated live e2e harnesses                                      |
| `make v1-proof`                                       | Protected V1 release proofs (fail-closed without credentials) |
| `make projection-rebuild`                             | Projection rebuild round-trip proof                           |

### CLI (`foundry`)

Run via:

```sh
docker compose -f deploy/docker-compose.yaml run --rm dev go run ./cmd/foundry <cmd>
```

| Command                                                                      | Description                          |
| ---------------------------------------------------------------------------- | ------------------------------------ |
| `foundry doctor`                                                             | Runtime connectivity checks          |
| `foundry keygen`                                                             | Create local Ed25519 approver keys   |
| `foundry login`                                                              | OIDC device-code login (strong auth) |
| `foundry plan submit\|approve\|verify\|revoke\|run`                          | Plan lifecycle                       |
| `foundry mission create\|start\|resume\|list\|status\|pause\|kill\|ceremony` | Mission ops / intake                 |
| `foundry intake show\|resume\|list`                                          | Intake run inspection                |
| `foundry opportunity list\|show\|report`                                     | Opportunity records                  |
| `foundry evidence verify\|show`                                              | Evidence bundles                     |
| `foundry cost` / `foundry budget`                                            | Cost ledger and envelopes            |
| `foundry migrate up\|down\|status`                                           | DB migrations                        |
| `foundry policy resolve`                                                     | Compile/resolve policy               |
| `foundry profile` / `foundry principal`                                      | Trust-domain / identity helpers      |

### Daemon (`foundryd`)

`foundryd` registers Temporal workers (DeliverPlan, MissionLoop, ImprovementLoop, TenX, portfolio) and serves the control-plane API. Run it when you need live workflow execution — not for unit tests.

---

## Architecture

```text
CLI (cmd/foundry)  ──HTTP──►  API (foundryd)
                                │
                     ┌──────────┼──────────┐
                     ▼          ▼          ▼
                 Temporal    Postgres   Evidence
                     │
                     ▼
              Kernel workflows
         DeliverPlan · MissionLoop · ImprovementLoop · TenX
                     │
        ┌────────────┼────────────┐
        ▼            ▼            ▼
   Sandbox exec   SCM write    Deploy / billing
   (default-deny) (kernel only) (personal track)
```

| Layer                                            | Responsibility                                         |
| ------------------------------------------------ | ------------------------------------------------------ |
| Kernel (`internal/kernel`, `internal/scm/write`) | Authority: sequencing, leases, budgets, side effects   |
| PEC (`internal/pec`)                             | Proposals only — prohibition-tested                    |
| Intake / input router                            | Authenticated intent → application path (no authority) |
| Mission / venture                                | Observe → evaluate → improve / roll back               |
| 10x                                              | ApprovedPlan → verified tasks → atomic handoff         |

### Repository layout

```text
the-foundry/
  cmd/foundry/          Operator CLI
  cmd/foundryd/         Daemon (Temporal workers + API)
  cmd/fitlint/          Fitness / topology lint tooling
  internal/             Kernel, mission, intake, policy, evidence, …
  internal/db/migrations/  Goose SQL (append-only, reversible)
  config/               Policy, profiles, allowlists, schemas
  deploy/               Dockerfile.dev, docker-compose.yaml, sandbox image
  docs/PLAN.md          Implementation plan (Tasks 1–152)
  docs/foundry/         Vendored V12 architecture set
  .ai/                  Canonical agent harness (compose → AGENTS.md / CLAUDE.md)
  test/e2e/             End-to-end harnesses
  evidence/             Task / gate evidence archives
  Makefile              Docker-wrapped make contract
  .env.example          Copy to .env (never commit secrets)
```

### Two tracks at a glance

| Concern          | Personal venture                      | Organization 10x                           |
| ---------------- | ------------------------------------- | ------------------------------------------ |
| Entry            | IDEA / MOCKUP / PLAN                  | Approved PLAN / org mockup                 |
| Terminal success | Mission result codes + deploy/observe | `SUCCEEDED` / `TEN_X_BRANCH_HANDOFF_READY` |
| SCM              | Product repo under policy             | CAS push to disposable/handoff branch      |
| Deploy           | Allowed under personal profile        | **Forbidden** on 10x path                  |
| Approval         | Strong auth for H-tier                | Stricter org provenance                    |

---

## Quality gates

Before you call a change done:

```sh
make bootstrap test lint fitness
make bootstrap doclint   # if docs / .ai / compose / Docker / workflows changed
```

If `.ai/` changed, recompose provider harnesses inside `dev`:

```sh
docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target codex
docker compose -f deploy/docker-compose.yaml run --rm dev ars compose --target claude
```

Never hand-edit composed `AGENTS.md` / `CLAUDE.md` — change `.ai/` and recompose.

---

## Documentation

| Doc                                                                          | Why read it                                |
| ---------------------------------------------------------------------------- | ------------------------------------------ |
| [docs/architecture.md](docs/architecture.md)                                 | One-page orientation + constitution table  |
| [docs/PLAN.md](docs/PLAN.md)                                                 | Task cards, milestones, execution protocol |
| [docs/foundry/delivery_foundry.md](docs/foundry/delivery_foundry.md)         | Normative V12 master index                 |
| [docs/foundry/docs/](docs/foundry/docs/)                                     | Workflows, autonomy, security, operations  |
| [docs/runbooks/profile-isolation.md](docs/runbooks/profile-isolation.md)     | Trust-domain deployment rule               |
| [docs/notes/v1-final-evidence-gate.md](docs/notes/v1-final-evidence-gate.md) | Latest V1 gate adjudication                |
| [`.ai/`](.ai/)                                                               | Agents, skills, instructions (ARES)        |

Suggested path: `docs/architecture.md` → `docs/PLAN.md` §A → `docs/foundry/delivery_foundry.md` → topic docs under `docs/foundry/docs/`.

---

## Contributing

1. Implement one `docs/PLAN.md` task card at a time (Scope / Out of scope are hard boundaries).
2. Run the card’s Validation commands, then `make test && make fitness`.
3. Migrations are append-only and must be reversible / down-tested.
4. Only `go-kernel` may touch `internal/kernel` side effects and `internal/scm/write` (Constitution C4).
5. PEC must never gain decision authority (Constitution C5) — fitness enforces this.
6. Do not add a fifth image lineage or a second compose file without a matching row in `docs/PLAN.md` §C.

Agent roles and skills: see [AGENTS.md](AGENTS.md) (composed from `.ai/`).
