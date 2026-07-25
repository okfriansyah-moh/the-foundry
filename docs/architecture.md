# Architecture Orientation

One page. For the full normative index, start at [`docs/foundry/delivery_foundry.md`](foundry/delivery_foundry.md).

## What this is

Delivery Foundry is a governed control plane for loop-engineered software delivery: a shared kernel owns state,
sequencing, recovery, evidence, policy, and cost accounting; a Plan Execution Coordinator (PEC) proposes work but
never decides. Two product tracks sit on top of the same kernel — Track A (personal autonomous venture) and
Track B (organization / 10x engineering).

## Constitution — non-negotiable articles

Full text and rationale: [`docs/PLAN.md`](PLAN.md) §B. Enforced by `make fitness` (enum lint, superseded-term lint,
import-boundary checks, PEC prohibitions, payload limits, doc-link check) with zero tolerance at milestone exits.

| #   | Article                                                                                                    |
| --- | ----------------------------------------------------------------------------------------------------------- |
| C1  | Exactly six workflow statuses; all richer meaning in registry-controlled `phase`/`reason`/`result_code`     |
| C2  | Temporal owns durable execution history, timers, sequencing                                                 |
| C3  | PostgreSQL workflow state = rebuildable projection, never execution authority                                |
| C4  | Kernel owns sequencing, retries, leases, fencing, state, policy, budgets, **all side effects incl. SCM writes** |
| C5  | PEC only proposes waves/dispatch/remediation; prohibition-tested                                              |
| C6  | Deterministic versioned AdmissionClassifier; a plan can never classify or authorize itself                    |
| C7  | ApprovedPlan provenance chain; authorship = provenance, never authorization                                   |
| C8  | Isolated worktrees; agents never touch canonical clones                                                      |
| C9  | External-operation ledger + idempotency keys for every side effect                                            |
| C10 | Evidence-based completion; no self-reported done                                                              |
| C11 | Telegram = notify/batch/flood-control/low-risk commands/veto digest; **never** high-risk approval             |
| C12 | Strong auth (OIDC+WebAuthn) for high-risk approvals                                                           |
| C13 | `personal-autonomous-venture` profile = explicit bounded production-auto grant                                |
| C14 | Organization/10x profile = stricter governance + provenance                                                   |
| C15 | 10x terminal `status: SUCCEEDED, result_code: TEN_X_BRANCH_HANDOFF_READY`; **no PR/merge/staging/deploy**     |
| C16 | Mockup = first-class entry with Observed/Inferred/Assumed/Unresolved labels                                   |
| C17 | Mission Setup Ceremony precedes unattended missions                                                           |
| C18 | Formal mission success/loop-exit semantics (result codes)                                                    |
| C19 | Cost accounting reserve→incur→reconcile; budgets enforced pre-execution; per-session caps                     |
| C20 | CumulativeChangeBudget governs autonomous L0/L1 promotion                                                     |
| C21 | Synthetic verification below canary threshold, honestly labeled                                               |
| C22 | Recovery/checkpoint/restart + honest `PROVEN_BLOCKED` on FAILED                                                |

## Link map into the vendored V12 doc set

| Topic                                    | Path                                                                                     |
| ----------------------------------------- | ----------------------------------------------------------------------------------------- |
| Master index, normative reading guide     | [`docs/foundry/delivery_foundry.md`](foundry/delivery_foundry.md)                        |
| State model (C1)                          | [`docs/foundry/docs/architecture/state-model.md`](foundry/docs/architecture/state-model.md) |
| Authority model — kernel vs PEC (C4, C5)  | [`docs/foundry/docs/architecture/authority-model.md`](foundry/docs/architecture/authority-model.md) |
| Data consistency (C2, C3)                 | [`docs/foundry/docs/architecture/data-consistency.md`](foundry/docs/architecture/data-consistency.md) |
| Plan provenance and approval (C7, C12)    | [`docs/foundry/docs/security/approval-and-provenance.md`](foundry/docs/security/approval-and-provenance.md) |
| Reviewer independence R0–R4               | [`docs/foundry/docs/security/reviewer-independence.md`](foundry/docs/security/reviewer-independence.md) |
| Admission tiers, classifier (C6)          | [`docs/foundry/docs/autonomy/admission-tiers.md`](foundry/docs/autonomy/admission-tiers.md) |
| Personal venture profile (C13)            | [`docs/foundry/docs/autonomy/personal-venture-profile.md`](foundry/docs/autonomy/personal-venture-profile.md) |
| Mission contract (C18)                    | [`docs/foundry/docs/autonomy/mission-contract.md`](foundry/docs/autonomy/mission-contract.md) |
| Cumulative drift governance (C20)         | [`docs/foundry/docs/autonomy/cumulative-drift-governance.md`](foundry/docs/autonomy/cumulative-drift-governance.md) |
| Cost accounting (C19)                     | [`docs/foundry/docs/operations/cost-accounting.md`](foundry/docs/operations/cost-accounting.md) |
| Telegram engine (C11)                     | [`docs/foundry/docs/operations/telegram.md`](foundry/docs/operations/telegram.md) |
| Disaster recovery, checkpoint/restart (C22) | [`docs/foundry/docs/operations/disaster-recovery.md`](foundry/docs/operations/disaster-recovery.md) |
| Quality rubric                            | [`docs/foundry/docs/governance/quality-rubric.md`](foundry/docs/governance/quality-rubric.md) |
| Implementation plan (tasks, milestones)   | [`docs/PLAN.md`](PLAN.md) |
| Agent harness (roles, skills, boundaries) | [`.ai/`](../.ai/) — composed into [`AGENTS.md`](../AGENTS.md) / [`CLAUDE.md`](../CLAUDE.md) |
| Prompt caching (per-provider mechanics + the stable-prefix rule) | [`.ai/instructions/prompt-caching.md`](../.ai/instructions/prompt-caching.md) |

Never feed [`docs/foundry/docs/legacy/`](foundry/docs/legacy/) to an implementation agent — it is banner-marked
superseded V11 history, preserved for archaeology only.

## Skill Catalog

Canonical source: [`.ai/skills/`](../.ai/skills/) (ARES format). Every skill is free-form Markdown under
`.ai/skills/<name>/SKILL.md`; which skills a given task needs is driven by the dispatched agent's `## Uses`
section in `.ai/agents/<role>/AGENT.md` — see the table below for the mapping. Composed into `AGENTS.md`/
`CLAUDE.md` by `ars compose`; never hand-edit the composed files.

| Skill | Purpose | Used by |
| --- | --- | --- |
| `task-implementation` | Implement one PLAN task card at a time, in Step order, self-checked against Acceptance. | all |
| `task-review` | Review a completed task against PLAN compliance, constitution, architecture, tests, security, complexity, release readiness. | all |
| `coding-standards` | Go conventions from `docs/PLAN.md` §C plus baseline idiom (Effective Go / Google Go Style Guide). | go-kernel, go-backend, infra, web |
| `code-quality` | KISS/YAGNI/DRY/SOLID and a concrete complexity budget. | go-kernel, go-backend |
| `stop-ai-slop` | Names and blocks AI-specific failure modes: placeholder code reported done, fabricated test results, scope drift, sycophantic reports, comment noise. | all |
| `security-hardening` | OWASP Top 10:2025 mapped to this codebase (access control, injection, supply chain, crypto, logging, etc.). | go-kernel, go-backend, integration, infra, web, security-review |
| `ai-vulnerability-defense` | OWASP Top 10 for LLM Applications (2025) mapped to this codebase — prompt injection, excessive agency, output handling, unbounded consumption. | go-kernel, integration, security-review |
| `code-review` | General seven-pillar review for diffs outside the task-card flow (PRs, remediation, dependency bumps). | security-review |
| `qa-testing` | Test pyramid (unit/integration/gated-e2e/fault-injection) and the required pre-done command set. | go-backend, integration, security-review |
| `frontend-development` | SvelteKit + Go API implementation discipline for the venture product template (Task 46+). | web |
| `ui-ux-design` | Visual/interaction design discipline — design tokens, WCAG 2.2 AA accessibility, honest mockup-fidelity labeling (C16). | web |

Security grounding: [OWASP Top 10:2025](https://owasp.org/Top10/2025/) (web application risks) and
[OWASP Top 10 for LLM Applications (2025)](https://owasp.org/www-project-top-10-for-large-language-model-applications/)
(LLM/agent-specific risks) — re-verify against the live source if either skill hasn't been reviewed in over a year.
