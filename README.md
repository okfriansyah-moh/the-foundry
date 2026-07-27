# Delivery Foundry

Delivery Foundry is a governed control plane for loop-engineered software delivery. It defines a durable, resumable, evidence-verified execution model for AI agents operating under explicit policy envelopes rather than implicit trust.

## Requirements

Docker + GNU make. Nothing else — no local Go, Node, or Playwright install is required. Every `make` target runs inside the `dev` toolchain image (see `deploy/Dockerfile.dev`, `deploy/docker-compose.yaml`).

```
make bootstrap test lint fitness
```

## Runtime services

`make up` brings up `postgres` and `temporal` (Task 4 / SKP-02) alongside `dev` on the shared
compose network. `dev` reaches them by service name (`postgres:5432`, `temporal:7233`) —
internal-network only, no host ports published, per the container topology table (which
also caps this compose file at exactly `dev`/`postgres`/`temporal` — no 4th service). Note:
`temporalio/auto-setup` has no built-in Web UI; a UI container is deferred to a future task.

```
make up      # start postgres + temporal
make doctor  # verify Docker/Compose are installed, then PG SELECT 1 + Temporal GetSystemInfo
make down    # stop and remove services (add KEEP_DATA=1 to keep the postgres volume)
```

## Executor sandbox (Task 34, rootless verification Task 97)

`internal/executor/sandbox` (`FOUNDRY_SANDBOX=oci`) runs executor commands in a container:
workspace read-write, `gomod-cache` read-only / `gobuild-cache` read-write, default-deny
network with a narrow, explicit egress allowlist (`config/sandbox-egress-allowlist.yaml`),
non-root user (`--user 10001:10001`), dropped capabilities, cgroup caps. Its tests
(`internal/executor/sandbox/*_test.go`, gated behind `RUN_SANDBOX=1`) launch real containers
and networks, so they run in **two bare-runner CI lanes** plus a local convenience lane:

- **`sandbox-tests` (authoritative, required for merge, unchanged by Task 97)** — the job in
  `.github/workflows/ci.yaml` runs directly on the GitHub Actions runner against plain
  (rootful, daemon-owned) Docker, no `dev` wrapper, no nested Docker daemon. It proves
  in-container non-root isolation (`--user`, `--cap-drop=ALL`) and the FS-jail/egress-allowlist
  topology, but — because Docker's daemon runs as host root — it does **not** prove
  engine-level rootlessness.
- **`sandbox-tests-rootless` (Task 97's new lane, also bare-runner)** — installs real rootless
  podman on the runner and re-runs the identical escape/legitimate-egress/cache-writability
  suite via `FOUNDRY_SANDBOX_TEST_ENGINE=podman`, plus a dedicated test
  (`rootless_test.go`'s `TestRootless_ContainerProcessRunsUnderUnprivilegedHostUID`) that
  inspects the HOST-side UID owning the container process — proving genuine user-namespace
  remapping, the property "rootless" actually names, with a negative control showing the same
  assertion fails against plain rootful Docker. As of this note, this job's green result has
  not yet been observed on a real GitHub Actions runner (see `docs/PLAN.md` Task 97's Status
  line for the current, precise verification state) — no rootless podman was available in the
  session that authored it either.
- **Local socket-mount lane (non-authoritative convenience only — never gates anything)** —
  from a host with Docker, run the sandbox tests from inside `dev` by mounting the host
  Docker socket in:

  ```
  RUN_SANDBOX=1 docker compose -f deploy/docker-compose.yaml run --rm \
    -v /var/run/docker.sock:/var/run/docker.sock dev \
    go test ./internal/executor/sandbox/...
  ```

  This does not run a Docker daemon inside `dev` (no privileged nested-daemon path) — it lets
  `dev`'s `docker` CLI drive the *host's* daemon directly through the mounted socket, exactly
  as any other Docker-Compose-style dev convenience does. Build the sandbox image once first:
  `docker build -f deploy/images/executor.Dockerfile -t foundry-executor-sandbox:latest .`
  Where real rootless podman is available locally instead, use
  `RUN_SANDBOX=1 FOUNDRY_SANDBOX_TEST_ENGINE=podman go test ./internal/executor/sandbox/...`
  directly (not through `dev`, for the same socket/nested-daemon reasons as above).

Execution protocol for implementing plan tasks: see [docs/PLAN.md](docs/PLAN.md) §A. Agent harness (roles, skills, boundaries) is canonically defined under [`.ai/`](.ai/) and composed into [`AGENTS.md`](AGENTS.md) / [`CLAUDE.md`](CLAUDE.md) — never hand-edit those two files; change `.ai/` and run `ars compose`.

## What this repository contains

This repository is organized around a V12 architecture and implementation blueprint for two product tracks:

- Track A: Personal Autonomous Venture Foundry
  - Accepts missions, mockups, requirements, specifications, or plans.
  - Supports bounded autonomous product creation, deployment, observation, and improvement.
- Track B: Organization / 10x Engineering Foundry
  - Accepts approved plans.
  - Supports multi-repository execution and direct shared-branch handoff workflows with stricter governance.

## Core documents

- [docs/foundry/delivery_foundry.md](docs/foundry/delivery_foundry.md) — master architecture, normative index, and reading guide
- [docs/foundry/CHANGELOG.md](docs/foundry/CHANGELOG.md) — version history and key V12 changes
- [docs/PLAN.md](docs/PLAN.md) — implementation plan with milestone-based tasks and execution rules
- [docs/foundry/V12_REVIEW_REPORT.md](docs/foundry/V12_REVIEW_REPORT.md) — review findings, validation results, and unresolved decisions
- [docs/architecture.md](docs/architecture.md) — one-page orientation: constitution table + link map into the vendored V12 doc set
- [docs/foundry/docs/](docs/foundry/docs/) — modular architecture, workflow, autonomy, security, operations, and governance documentation
- [.ai/](.ai/) — canonical agent harness (ARES format): six agents, eleven skills (coding standards, code
  quality, anti-slop, security hardening, AI/LLM-vulnerability defense, code review, QA, frontend, UI/UX — see
  [docs/architecture.md](docs/architecture.md#skill-catalog) for the full catalog and who uses what),
  instructions, prompts

## Key ideas

- A shared kernel owns state, sequencing, recovery, evidence, policy, and cost accounting.
- The Plan Execution Coordinator (PEC) proposes work, while the kernel retains authority over state and side effects.
- Deterministic admission, provenance, reviewer independence, and recovery semantics are first-class contracts.
- Autonomy is bounded by explicit profiles, budgets, mission contracts, and human touchpoints.

## Current status

The V12 documentation set has been delivered as a modular architecture package. The review report indicates that the main acceptance criteria for the architecture and workflow relocation were met, while the size-growth acceptance criterion was intentionally reported as not met due to the documented trade-off between preserving V11 content and adding new normative contracts.

## Suggested reading path

1. Start with [docs/foundry/delivery_foundry.md](docs/foundry/delivery_foundry.md)
2. Review the implementation roadmap in [docs/PLAN.md](docs/PLAN.md)
3. Read [docs/foundry/CHANGELOG.md](docs/foundry/CHANGELOG.md) and [docs/foundry/V12_REVIEW_REPORT.md](docs/foundry/V12_REVIEW_REPORT.md) for context and validation history
4. Drill into [docs/foundry/docs/](docs/foundry/docs/) for the detailed contracts and workflows

## Running the plan autonomously

`tools/planrunner` (Task 3 / RUN-01) is a standalone bootstrap tool — outside `internal/`,
never part of the shipped `foundry`/`foundryd` binaries — that drives `docs/PLAN.md`
end to end so a human doesn't have to trigger every task by hand. It never touches
`internal/*` directly; it only invokes the same implementation protocol a human would
run manually (headless `claude -p`, the task's own Validation commands, then repo-wide
`make test fitness`).

```
make plan-run
```

Each eligible task's `Risk`/`Rev` fields (already on every card, not a new classifier —
Constitution C6) decide the path:

- **AUTO** (`Risk: Low`/`Med` and `Rev: R1`/`R2`) — implemented, validated, committed, and
  merged with zero human steps. Completions are reported via a non-blocking batched
  Telegram digest (every 5 completions or 2 hours, whichever is first).
- **GATED** (`Risk: High` or `Rev: R3`/`R4`) — implemented and validated, then the runner
  stops before committing and sends a blocking Telegram message naming the task, changed
  files, validation results, and the exact gating reason. It waits for `/approve <task>`
  or `/reject <task>`; no reply within the configured window means it stays paused — it
  never auto-approves.
- Two consecutive validation failures on the same task halt the entire runner with a
  Telegram alert rather than retrying silently. A `/freeze` command halts immediately.

Configure the disposable bootstrap Telegram bot via `.env` (`TELEGRAM_BOT_TOKEN`,
`TELEGRAM_CHAT_ID` — see `.env.example`); this bot is explicitly throwaway and distinct
from Foundry's eventual production Telegram engine (Task 30).

**Exit condition — retire this tool.** Once Foundry's own kernel (Task 12), deterministic
classifier (Task 7), and Telegram engine (Task 30) exist, stop using this runner for new
tasks. Admit the remaining `docs/PLAN.md` backlog as real `ApprovedPlan` documents
executed by Foundry itself instead — the dogfooding the plan's own thesis points at. This
tool's job ends at that point; it does not keep running alongside the real kernel.

## Next step

Use this repository as the source of truth for the Delivery Foundry architecture, governance model, and implementation plan. If you want to build the system, begin with the milestones and tasks defined in [docs/PLAN.md](docs/PLAN.md).
