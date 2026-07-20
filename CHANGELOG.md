# Delivery Foundry — Document Change Log

[← Back to Delivery Foundry master index](delivery_foundry.md)

## Version 12.0 — 2026-07-19

Structural: decomposed the single 14,293-line V11 file into a master index plus modular documents per N20; every V11 section relocated (see `docs/MIGRATION_MAP_V11_TO_V12.md`); historical state names normalized throughout relocated content (this changelog's older entries were normalized too — original wording is recoverable from V11).

Contradictions resolved: single canonical state model with phase/reason/result_code registries and a superseded historical mapping (`TEN_X_BRANCHES_READY` now only a deprecated alias of result_code `TEN_X_BRANCH_HANDOFF_READY`; `PROVEN_BLOCKED` formally `status: FAILED, result_code: PROVEN_BLOCKED`); kernel/PEC authority table replaces conflicting "Forge owns scheduling and state" language; `Forge` renamed **Plan Execution Coordinator (PEC)** to avoid the Atlassian Forge collision; stale "Version 10" self-references neutralized.

Autonomy engineering added: deterministic AdmissionClassifier with tiers A0/A1/A2/H and self-classification fitness rule; BillingMaturity (billing Tier H until proven); CanarySignalPolicy with synthetic verification below traffic threshold; L0–L4 promotion levels, CumulativeChangeBudget, weekly non-blocking veto digest; Mission Setup Ceremony with MissionReadinessArtifact and `reason: unforeseen-human-gate`; MissionContract with mission result codes and universal loop contracts for all eight loops; mockup as a first-class entry (D-28); explicit `personal-autonomous-venture` profile as an authorized N13 override; human-touchpoint inventory with autonomy metrics.

Engineering contracts added: plan provenance and ApprovedPlan artifact chain; reviewer independence R0–R4 with anti-rubber-stamp metrics; Temporal/PostgreSQL consistency contract (D-30); ADR-000 build-versus-buy; cost accounting and mission economics; control-plane self-protection; observability SLO catalog with payload limits; retention/PII classes (UU PDP); configuration-compiler vs OPA responsibility split; dual-track roadmap (D-29) with Shared Kernel Proof and two Minimum Lovable Slices; realistic sizing with declared team assumptions.

Governance: V11's uniform 10/10 self-scorecard retired in favor of evidence-based per-dimension scoring; new documentation lint gates (superseded-term, second-enum, duplicate-contract, duplicate-diagram-ID).

The preserved V11 change history follows (state names normalized).


---

<!-- Relocated from V11: §31 Document change log (lines 14092-14293) -->


### Version 11.0

Added Mermaid diagram coverage across the complete architecture:

- Diagram policy requiring every workflow, stateful process, security boundary, recovery path, and side-effect process to have a Mermaid representation.
- Diagram coverage matrix and stable diagram identifiers.
- System context and control-plane boundary.
- Nested Loop Engineering model covering portfolio, delivery, recovery, capacity, capability, learning, memory, and security loops.
- Reference runtime-component architecture.
- Canonical domain relationship model.
- Workflow lifecycle state diagram and control-plane execution sequence.
- Configuration precedence and compilation.
- Unified extension lifecycle and taxonomy.
- Prompt-injection, policy, tool-gateway, secret, sandbox, and audit sequence.
- External-operation saga and reconciliation lifecycle.
- Repository mirror, worktree, and branch-delivery process.
- Provider capacity, compaction, wait, retry, rollover, and failover.
- Operator/API interaction.
- Auto, command, and disabled deployment resolution.
- Data, artifact, audit, notification, and memory flow.
- Liveness, orphan recovery, and disaster-recovery sequence.
- Verification and evidence acceptance pipeline.
- Reference workflow family.
- Capability maturity progression.
- Milestone-gated implementation roadmap.
- Documentation architecture.
- ADR governance and solidity feedback loop.
- Direct PLAN multi-repository execution.
- 10x Implementation Branch Mode.
- Telegram batching and flood-control recovery.
- Capability evolution, self-learning, and memory.
- Venture portfolio and product loop.
- Documentation CI requirements for Mermaid syntax, process coverage, state-model consistency, and diagram links.

### Version 10.0

Refactored for architectural solidity:

- Added an authoritative normative architecture before the preserved brainstorming compendium.
- Chose a concrete reference implementation: Go control plane, Temporal workflow backend, PostgreSQL, S3-compatible artifacts, OPA-compatible policy, Vault/KMS-compatible secrets, rootless OCI runners, and OpenTelemetry.
- Defined the trusted computing base, identity model, policy integrity, public/private boundary, and signed configuration artifacts.
- Added one canonical domain model and source-of-truth ownership table.
- Replaced the growing global workflow enum with a generic lifecycle plus typed phase, reason, and result codes.
- Added formal configuration precedence and a resolved-policy compiler that rejects attempts to weaken higher-level policy.
- Unified plugins, capabilities, adapters, executors, agents, skills, and methodology packs into one extension taxonomy and registry lifecycle.
- Repositioned Make as a thin CLI wrapper; API and `foundry` CLI are now canonical.
- Added an external-operation ledger, saga semantics, compensation, reconciliation, and divergence states for multi-system side effects.
- Added provider execution classes separating API automation, unattended CLI, attended subscriptions, managed sessions, local runtimes, and unsupported surfaces.
- Defined environment revision provenance so integration evidence cannot pass against an unrelated deployment.
- Renamed the 10x direct-push workflow to 10x Implementation Branch Mode and defined `TEN_X_BRANCH_HANDOFF_READY`.
- Changed direct-push default from per-task to per-atomic-group unless intermediate branch invariants prove every task is independently buildable.
- Set safe deployment defaults: preview auto, staging command, production command.
- Added a concrete API, data architecture, deployment topologies, SLOs, RPO/RTO, disaster recovery, and audit-integrity expectations.
- Added kernel property tests, fault injection, security evaluations, architecture fitness functions, and an AI-slop prevention gate.
- Classified every brainstormed capability as normative, supported, experimental, or deferred instead of deleting it.
- Added a milestone-gated implementation roadmap beginning with one direct-PLAN, one-repository vertical slice.
- Added a documentation extraction architecture so provider, Telegram, security, recovery, and workflow details can move into maintainable modules.
- Added a supersession map resolving contradictions from earlier iterations.
- Sanitized personal and organization-derived identifiers in the public blueprint.
- Added a scorecard explaining the evidence required to earn a 10/10 implementation rather than claiming it from prose alone.

### Version 9.0

Added:

- First-class `10x` shared-branch direct-push operating mode.
- Public example profile `organization-10x-direct-push`.
- Workflow that starts from an approved `PLAN.md`, uses existing `10x-branch` targets, and stops at branch readiness.
- Explicit no-PR, no-merge, and no-deployment delivery boundary.
- Local-only task worktrees so parallel agents never share one checkout and never create remote task branches.
- Deterministic Branch Integrator with per-repository leases, serialized push queues, remote-SHA drift detection, commit replay, post-replay checks, and force-push prohibition.
- Configurable direct-push cadence with `after-accepted-task` as the 10x default.
- Branch-based multi-repository change-set manifest containing branch heads and push receipts instead of PR URLs.
- Review and quality gates that work directly against branch diffs without requiring a pull request.
- Configuration-toggle guard based on the real 10x lesson that temporary tenant settings must be restored or explicitly handed off.
- Integration-without-deployment policy using local execution or already-existing approved endpoints.
- Terminal `TEN_X_BRANCH_HANDOFF_READY` status and extractable later release workflow.
- Telegram commands, notifications, Makefile targets, rollout requirements, and definition of done for 10x execution.
- Detailed real scenario using `acme-tools-dashboard`, `acme-internal-tools-service`, `acme-mail-service`, and `acme-company-settings`, all targeting `10x-branch`.

### Version 8.0

Added:

- Direct `PLAN.md` execution as a first-class entry path beside mission and requirement workflows.
- PLAN upload through CLI, Telegram, API, repository, Jira, Confluence, and artifact references.
- Artifact classification that distinguishes executable plans, draft plans, specifications, missions, and unknown documents.
- Admission validation for tasks, waves, repositories, file conflicts, acceptance evidence, commands, permissions, integration, deployment, and rollback.
- Safe structural PLAN repair while preserving human authority over semantics and scope.
- Multi-repository PLAN schema with repository aliases, responsibilities, task mappings, branching, change-set, merge, rollback, integration, and deployment configuration.
- Repository resolution order, profile allowlist enforcement, and evidence-based missing-repository proposals.
- Reusable read-only Git mirrors and deterministic multi-repository Workspace Manager.
- Configurable branch strategies: per task, group, repository, repository-group, change set, and existing branch.
- Cross-repository contract freezing and parallel consumer execution.
- Integration manifests and multi-service test environments.
- Durable linked pull-request change sets with merge and rollback ordering.
- Provider/session restart that resumes an individual task or wave without restarting the entire plan.
- Complete direct-plan notification events and Telegram `/run-plan` and repository commands.
- Full worked scenario showing an uploaded PLAN spanning web, API, notification, and infrastructure repositories from intake through automatic deployment.

### Version 7.0

Added:

- Telegram-specific flood-control design based on official private-chat, group, global broadcast, text-length, and `retry_after` behavior.
- Conservative configurable internal ceilings below Telegram's documented guidance.
- Hierarchical bot, chat-type, chat, and priority token buckets.
- Durable adaptive batching so every process event is preserved without sending one Telegram message per event.
- Step-message editing, coalescing windows, digests, priority preemption, and weighted-fair chat scheduling.
- Rendered-length-safe message chunking with summary-and-artifact overflow.
- Dynamic HTTP 429 handling that treats Telegram's `retry_after` as authoritative.
- Configurable fallback jittered exponential backoff when no retry time is returned.
- Unlimited notification lifetime with bounded immediate and repeated-same-error retries.
- Learned Telegram throughput calibration that decreases rapidly after flood control and increases cautiously after clean windows.
- Fallback channels, bounded Telegram recovery digests, delivery receipts, and command-message delivery requirements.
- Telegram health states, Makefile commands, flood simulations, and multi-chat load tests.
- Expanded definition of done requiring no configured private, group, or global rate violations.

### Version 6.0

Added:

- Stable plug-and-play kernel separating immutable orchestration services from replaceable integrations.
- Versioned plugin kinds for SCM, tracker, knowledge, research, planning, execution, review, verification, browser, notification, deployment, memory, models, methodology packs, skills, security, and optimization.
- Plugin manifests, immutable locks, dependency resolution, lifecycle, adapter generation, conformance tests, shadow, canary, activation, deprecation, and revocation.
- Future-GitHub-project onboarding that can discover a repository, generate an adapter, test compatibility, and safely propose activation.
- Declarative versioned workflow graph with logical role bindings rather than named-component coupling.
- Swappable, removable, conditional, and independently executable workflow steps.
- Step execution modes: `auto`, `command`, `manual-trigger`, `manual-execution`, `external`, `shadow`, `dry-run`, and `disabled`.
- Standalone step CLI/API and manual task packet contracts.
- Config-driven deployment for preview, staging, and production.
- Deployment default changed to `auto`; `command` mode waits for an authenticated Telegram command with no mandatory timeout.
- Complete process notification policy covering every workflow step, progress milestone, checkpoint, retry, wait, provider transition, plugin lifecycle, deployment, learning, capacity, and security event.
- Durable notification outbox, delivery receipts, message update/deduplication, retries, fallback channels, and dead-letter queue.
- Authorized, state-aware, nonce-bound, replay-protected Telegram command gateway.
- Updated personal and organization workflows so merge and deployment behavior come from workflow/profile configuration rather than hardcoded assumptions.

### Version 5.0

Added:

- Provider-capacity control plane covering request rate, token throughput, context capacity, subscription allowance, spend, and runtime slots.
- Dynamic capacity snapshots from provider APIs, response headers, native CLI status, error reset times, billing integrations, and conservative observations.
- Anthropic API rate-limit headers and Rate Limits API integration.
- Subscription-aware handling for Claude Code, Codex, Cursor, and GitHub Copilot.
- Capacity reservations, pre-exhaustion draining, adaptive concurrency, health scoring, and task resizing.
- Durable provider-neutral task packets and session checkpoints.
- Same-session resume, compaction, fresh-session rollover, subscription-to-API fallback, cross-provider restart, batch conversion, local fallback, and wait-until-reset modes.
- Task leases, heartbeats, fencing tokens, stale-worker recovery, and durable wake-up scheduling.
- Retry policy that permits unlimited workflow lifetime while bounding identical failures, strategy attempts, request frequency, cost, and no-progress loops.
- Liveness Supervisor with explicit non-stall invariants, orphan repair, no-progress detection, and durable PostgreSQL scheduling.
- Honest Non-Stall Guarantee and conditional Eventual Completion Guarantee.
- `PROVEN_BLOCKED` terminal outcome for unsatisfiable or permanently unavailable work.
- Capacity-aware self-learning for task sizing, forecasting, concurrency, reset scheduling, and fallback order.
- Makefile commands and rollout tests for capacity, checkpoint, restart, retry, and liveness.

### Version 4.0

Added:

- Provider-neutral LLM Capability Optimization Layer and execution-envelope compiler.
- Runtime model/capability discovery with GA, beta, research-preview, deprecated, and retired feature policy.
- Anthropic capability profile covering adaptive thinking, effort, orchestration mode, task budgets, prompt caching, cache diagnostics, compaction, context editing, tool search, programmatic tool calling, strict tools, structured outputs, advisor, fast mode, batch processing, server tools, Files, PDF, vision, memory, Agent Skills API, MCP, token counting, tool runner, and optional Managed Agents.
- Explicit feature-interaction policies and rules for native capabilities, Headroom, and 9Router.
- LLM Capability Optimizer agent, task profiles, telemetry, replay, shadow, canary, promotion, and rollback.
- Native-capability security controls for cache, compaction, tool search, code execution, skills, MCP, and managed sessions.
- Superpowers methodology-pack integration with pinned source, conflict resolution, provider installation, behavioral evaluation, and security controls.
- Mapping that adopts Superpowers brainstorming, worktrees, TDD, systematic debugging, verification, review, branch completion, and skill-writing while keeping PEC as the sole plan executor.
- Makefile surface for capability discovery, context optimization, benchmarking, and methodology-pack lifecycle.
- Worked autonomous SaaS example showing phase-specific LLM capability envelopes.

### Version 3.0

Added:

- Capability Evolution Loop for discovering, generating, quarantining, evaluating, staging, promoting, revoking, and rolling back agents and skills.
- Immutable security control plane separate from the adaptive learning plane.
- Prompt-injection threat model covering direct and indirect injection from web pages, code, issues, documentation, logs, packages, tools, and memory.
- Trust-labelled context, taint propagation, prompt firewall, deterministic tool gateway, and injection regression fixtures.
- npm, library, package, action, and malware supply-chain controls including lockfiles, provenance, signatures, default-deny lifecycle scripts, SBOMs, immutable action references, and sandbox evaluation.
- Ephemeral least-privilege sandboxes, capability tokens, secret broker, default-deny egress, and cross-profile isolation.
- Durable multi-layer memory with event provenance, memory promotion, contradiction handling, retention, redaction, and poisoning defenses.
- Bounded self-healing with recovery levels, failure classification, circuit breakers, rollback, quarantine, and escalation.
- Evidence-driven self-learning, shadow evaluation, canary promotion, adaptive routing, and immutable non-adaptive policy boundaries.
- Capability Curator, Recovery Manager, and Memory Curator agents.
- Security, capability, memory, and recovery Makefile targets and expanded definition of done.

### Version 2.0

Added:

- canonical separation of agents, skills, references, domain skills, and runtime adapters;
- normalized package layout based on the supplied agent and skill examples;
- planning, PEC, implementation, backend, reviewer, and verification agent roles;
- agent and skill catalogs;
- platform-level and product-local package locations;
- Makefile targets for installation, validation, and execution;
- exact rules for waves, task sizing, evidence, retries, review, and verification;
- complete personal B2B SaaS mission from initial instruction through discovery, validation, product selection, frontend/API/MCP construction, release, and growth;
- public-safe naming and profile-specific package permissions.

