# Delivery Foundry

Delivery Foundry is a governed control plane for loop-engineered software delivery. It defines a durable, resumable, evidence-verified execution model for AI agents operating under explicit policy envelopes rather than implicit trust.

## Requirements

Docker + GNU make. Nothing else — no local Go, Node, or Playwright install is required. Every `make` target runs inside the `dev` toolchain image (see `deploy/Dockerfile.dev`, `deploy/docker-compose.yaml`).

```
make bootstrap test lint fitness
```

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

## Next step

Use this repository as the source of truth for the Delivery Foundry architecture, governance model, and implementation plan. If you want to build the system, begin with the milestones and tasks defined in [docs/PLAN.md](docs/PLAN.md).
