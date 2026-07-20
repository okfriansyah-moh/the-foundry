# Delivery Foundry

Delivery Foundry is a governed control plane for loop-engineered software delivery. It defines a durable, resumable, evidence-verified execution model for AI agents operating under explicit policy envelopes rather than implicit trust.

## What this repository contains

This repository is organized around a V12 architecture and implementation blueprint for two product tracks:

- Track A: Personal Autonomous Venture Foundry
  - Accepts missions, mockups, requirements, specifications, or plans.
  - Supports bounded autonomous product creation, deployment, observation, and improvement.
- Track B: Organization / 10x Engineering Foundry
  - Accepts approved plans.
  - Supports multi-repository execution and direct shared-branch handoff workflows with stricter governance.

## Core documents

- [delivery_foundry.md](delivery_foundry.md) — master architecture, normative index, and reading guide
- [CHANGELOG.md](CHANGELOG.md) — version history and key V12 changes
- [PLAN_7.md](PLAN_7.md) — implementation plan with milestone-based tasks and execution rules
- [V12_REVIEW_REPORT.md](V12_REVIEW_REPORT.md) — review findings, validation results, and unresolved decisions
- [docs/](docs/) — modular architecture, workflow, autonomy, security, operations, and governance documentation

## Key ideas

- A shared kernel owns state, sequencing, recovery, evidence, policy, and cost accounting.
- The Plan Execution Coordinator (PEC) proposes work, while the kernel retains authority over state and side effects.
- Deterministic admission, provenance, reviewer independence, and recovery semantics are first-class contracts.
- Autonomy is bounded by explicit profiles, budgets, mission contracts, and human touchpoints.

## Current status

The V12 documentation set has been delivered as a modular architecture package. The review report indicates that the main acceptance criteria for the architecture and workflow relocation were met, while the size-growth acceptance criterion was intentionally reported as not met due to the documented trade-off between preserving V11 content and adding new normative contracts.

## Suggested reading path

1. Start with [delivery_foundry.md](delivery_foundry.md)
2. Review the implementation roadmap in [PLAN_7.md](PLAN_7.md)
3. Read [CHANGELOG.md](CHANGELOG.md) and [V12_REVIEW_REPORT.md](V12_REVIEW_REPORT.md) for context and validation history
4. Drill into [docs/](docs/) for the detailed contracts and workflows

## Next step

Use this repository as the source of truth for the Delivery Foundry architecture, governance model, and implementation plan. If you want to build the system, begin with the milestones and tasks defined in [PLAN_7.md](PLAN_7.md).
