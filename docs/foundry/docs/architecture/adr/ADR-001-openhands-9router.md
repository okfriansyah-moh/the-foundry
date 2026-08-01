# ADR-001 — OpenHands / 9Router disposition

[← Back to Delivery Foundry master index](../../../delivery_foundry.md) · [Migration map](../../../docs/MIGRATION_MAP_V11_TO_V12.md)

Status: Accepted. OpenHands and 9Router are deferred optional pluggable externals, not core architecture.

Date: 2026-07-31.

## Context

OpenHands and 9Router appear in normative provider, workflow, and configuration documents, but there is no
implemented adapter for either and no current core dependency on either. Blocker B14 resolved the architectural
question: both are external execution or routing options that may fit behind the existing
`internal/executor.Adapter` seam, but neither belongs in the core architecture now.

Headroom is deliberately outside this decision and remains separately undecided.

## Criteria applied

| Criterion | 9Router | OpenHands |
|---|---|---|
| Adds a capability no existing adapter has? | No. Task 129's in-allowlist bounded fallback already satisfies the §18 fail-closed fallback intent without adding a proxy. | No. It does not add dispatch capability beyond the existing CLI-adapter executor class, `internal/pec` wave proposals, and Task 124 concurrent wave dispatch. |
| Fits the capability registry without special-casing? | Yes, as a future model-routing adapter behind the existing seam. | Yes, as a future executor adapter behind the existing seam. |
| Can satisfy Task 115 sandbox and Task 116 allowlist constraints? | Not for organization data without additional approval: proxy routing is forbidden by policy for that data class and requires the operating organization's own security approval. | Only if a future adapter runs through the mandatory sandbox and allowlist enforcement like every other executor. |

## Decision

Delivery Foundry rejects core adoption of OpenHands and 9Router for now. Both are deferred as optional pluggable
adapters behind the existing, unchanged `internal/executor.Adapter` contract. No Go interface, capability registry
entry, policy rule, or adapter package is added by this decision.

Normative references may keep legacy examples or future-facing examples, but they must be annotated so a reader does
not mistake either system for planned core work.

## Consequences

- The core architecture remains provider-neutral and keeps the existing executor seam unchanged.
- Task 129's in-allowlist fallback is the current answer to the fail-closed fallback requirement.
- Organization data must not be routed through an external proxy merely because one is technically available.
- Future OpenHands or 9Router work is an adapter task, not an architecture prerequisite.

## Revisit conditions

Reopen this decision only if a concrete capability gap appears that no in-allowlist executor or direct provider
adapter can satisfy, and if the proposed adapter can pass the mandatory sandbox, allowlist, policy, and shared
executor-contract requirements without special-casing. Any proxy route for organization data also requires the
operating organization's own security approval.
