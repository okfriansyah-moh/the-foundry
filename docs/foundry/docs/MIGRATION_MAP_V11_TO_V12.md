# Migration Map — V11 → V12

[← Back to Delivery Foundry master index](../delivery_foundry.md)

Every V11 section, its destination, and its status. Line numbers refer to `delivery_foundry_10_.md` (V11, 14,293 lines).

| V11 section (lines) | V12 destination | Status |
|---|---|---|
| V11 header (lines 1–29) (1–29) | `—` | replaced by new master index (delivery_foundry.md) |
| Part I preamble (30–37) | `—` | replaced by master index framing |
| N0 How to read this specification (38–115) | `delivery_foundry.md` | retained |
| §20.5 Nested Loop Engineering model (11985–12028) | `delivery_foundry.md` | retained (promoted to master) |
| N17 Reference workflows (1612–1718) | `delivery_foundry.md` | retained + mockup/specification entries added |
| N1 Architectural thesis (116–272) | `docs/architecture/overview.md` | retained |
| N2 Scope and non-goals (273–309) | `docs/architecture/overview.md` | retained |
| N3 Reference implementation architecture (310–439) | `docs/architecture/overview.md` | retained |
| N12 API and operator interfaces (1229–1298) | `docs/architecture/overview.md` | retained |
| §4 Architecture (2537–2569) | `docs/architecture/overview.md` | retained |
| §5–5.3 Repository model (2570–3145) | `docs/architecture/overview.md` | retained |
| §29 Final operating model (13902–13958) | `docs/architecture/overview.md` | retained |
| P0 Process diagram atlas (2131–2274) | `docs/architecture/overview.md` | retained + D-28..D-31 registered |
| N4 Canonical domain model (440–535) | `docs/architecture/domain-model.md` | retained |
| N5 Workflow semantics (536–678) | `docs/architecture/state-model.md` | retained (registries formalized in front matter) |
| N7 Unified extension model (781–884) | `docs/architecture/authority-model.md` | retained + PEC added to taxonomy |
| §5.4 Canonical agent and skill packaging (3146–3659) | `docs/architecture/authority-model.md` | retained (forge→pec renamed) |
| §5.10 Plug-and-play plugin kernel (5018–5310) | `docs/architecture/authority-model.md` | retained |
| §28.1 Supplied agent and skill examples (13878–13901) | `docs/architecture/authority-model.md` | retained (forge→pec renamed) |
| N6 Configuration and policy compilation (679–780) | `docs/architecture/configuration-and-policy.md` | retained |
| N13 Safe deployment defaults (1299–1364) | `docs/architecture/configuration-and-policy.md` | retained (personal profile grant added in autonomy/) |
| §3 Public profile taxonomy (2380–2404) | `docs/architecture/configuration-and-policy.md` | retained |
| §5.11 Declarative and swappable workflow graph (5311–5625) | `docs/architecture/configuration-and-policy.md` | retained |
| §7 Dynamic target configuration (7003–7168) | `docs/architecture/configuration-and-policy.md` | retained |
| §8 Profile schema (7169–7427) | `docs/architecture/configuration-and-policy.md` | retained |
| N9 External operations, sagas, reconciliation (966–1062) | `docs/architecture/external-operations.md` | retained |
| N14 Data architecture (1365–1449) | `docs/architecture/data-consistency.md` | retained + consistency contract added |
| §20.1 Durable memory architecture (11625–11756) | `docs/architecture/data-consistency.md` | retained |
| §20.6 Memory Curator (12029–12058) | `docs/architecture/data-consistency.md` | retained |
| N22 Architecture decision records (2045–2086) | `docs/architecture/adr/README.md` | retained + ADR-000 added |
| §5.12 Direct PLAN.md execution (5626–6297) | `docs/workflows/direct-plan.md` | retained (provenance now in security/approval-and-provenance.md) |
| §13.8 Worked scenario: four-repository PLAN (8592–8971) | `docs/workflows/direct-plan.md` | retained |
| N10 Repository and workspace model (1063–1156) | `docs/workflows/multi-repository.md` | retained |
| §15 Organization engineering workflow (10240–10555) | `docs/workflows/multi-repository.md` | retained |
| §5.13 10x shared-branch direct-push execution (6298–6749) | `docs/workflows/ten-x-branch.md` | retained (terminal result normalized) |
| §13.9 Real scenario: 10x across four shared branches (8972–9322) | `docs/workflows/ten-x-branch.md` | retained (status line normalized) |
| §14 Personal venture workflow (9323–10239) | `docs/workflows/venture-loop.md` | retained (contracts referenced in front matter) |
| §5.5 Capability Evolution Loop (3660–3941) | `docs/workflows/capability-evolution.md` | retained (auto-promotion governed by autonomy/cumulative-drift-governance.md) |
| §5.6 Secure self-improvement kernel (3942–3991) | `docs/workflows/capability-evolution.md` | retained |
| §5.9 Superpowers methodology pack (4833–5017) | `docs/workflows/capability-evolution.md` | retained (forge→pec renamed) |
| §20.2 Self-healing (11757–11876) | `docs/workflows/recovery.md` | retained |
| §20.9 Retry policy without stalling (12583–12697) | `docs/workflows/recovery.md` | retained |
| §20.11 Honest completion guarantee (12778–12838) | `docs/workflows/recovery.md` | retained (reviewer levels formalized in security/reviewer-independence.md) |
| §21 Retry and idempotency (12951–12979) | `docs/workflows/recovery.md` | retained |
| §16 Human involvement by profile (10556–10575) | `docs/autonomy/human-touchpoints.md` | retained (superset inventory added in front matter) |
| §20.3 Self-learning (11877–11958) | `docs/autonomy/cumulative-drift-governance.md` | retained (promotion levels formalized) |
| §20.4 Self-adaptation (11959–11984) | `docs/autonomy/cumulative-drift-governance.md` | retained (bounded auto-promotion added) |
| N8 Trusted computing base and security (885–965) | `docs/security/authorization-model.md` | retained |
| §12 Credential strategy (7929–7972) | `docs/security/authorization-model.md` | retained |
| §13.1 Security-by-design threat model (8010–8059) | `docs/security/authorization-model.md` | retained |
| §13.2 Prompt-injection defense (8060–8237) | `docs/security/authorization-model.md` | retained |
| §13.4 Runtime sandbox and least privilege (8400–8485) | `docs/security/authorization-model.md` | retained |
| §13.5 Cross-profile isolation (8486–8503) | `docs/security/authorization-model.md` | retained |
| §13.6 Security state machine (8504–8538) | `docs/security/authorization-model.md` | retained |
| §13.7 Native LLM capability security (8539–8591) | `docs/security/authorization-model.md` | retained |
| §13.3 Package, npm, library, malware defense (8238–8399) | `docs/security/supply-chain.md` | retained (dependency changes never A0) |
| §17 Data classification policy (10576–10622) | `docs/security/data-retention-and-privacy.md` | retained (retention classes added) |
| §20.7 Provider-capacity awareness (12059–12397) | `docs/operations/capacity.md` | retained |
| §20.12 Capacity-aware self-learning (12839–12950) | `docs/operations/capacity.md` | retained |
| §13 Webhook ingestion (7973–8009) | `docs/operations/control-plane-protection.md` | retained |
| N15 Observability, SLOs, disaster recovery (1450–1528) | `docs/operations/observability-and-alerts.md` | retained (metric catalog added) |
| §19 Complete process notification policy (10648–11544) | `docs/operations/telegram.md` | retained (Telegram never approves high-risk actions — see approval-and-provenance) |
| §20.8 Durable checkpoint, restart, resume (12398–12582) | `docs/operations/disaster-recovery.md` | retained |
| §20.10 Liveness Supervisor (12698–12777) | `docs/operations/disaster-recovery.md` | retained (ORPHANED mapped as supervisor condition) |
| §9–9.3 Root Makefile interface (7428–7825) | `docs/operations/cli-and-makefile.md` | retained |
| §10 Makefile configuration flow (7826–7890) | `docs/operations/cli-and-makefile.md` | retained |
| §22 CI portability (12980–13009) | `docs/operations/cli-and-makefile.md` | retained |
| §24 Makefile bootstrap sequence (13034–13068) | `docs/operations/cli-and-makefile.md` | retained |
| §25 Detailed make doctor (13069–13138) | `docs/operations/cli-and-makefile.md` | retained |
| §25.1 Security/capability/memory/recovery Makefile surface (13139–13211) | `docs/operations/cli-and-makefile.md` | retained |
| §25.2 Capacity/checkpoint/liveness Makefile surface (13212–13287) | `docs/operations/cli-and-makefile.md` | retained |
| §5.8 Anthropic capability profile (4149–4832) | `docs/providers/anthropic.md` | retained (staleness banner; verify at implementation) |
| N11 Provider execution classes (1157–1228) | `docs/providers/provider-execution-classes.md` | retained |
| §5.7 LLM Capability Optimization Layer (3992–4148) | `docs/providers/provider-execution-classes.md` | retained |
| §6–6.7 Provider abstraction contracts (6750–7002) | `docs/providers/provider-execution-classes.md` | retained |
| §11 Adapter selection (7891–7928) | `docs/providers/provider-execution-classes.md` | retained |
| §18 Model-routing policy (10623–10647) | `docs/providers/provider-execution-classes.md` | retained |
| N16 Verification and test strategy (1529–1611) | `docs/governance/quality-rubric.md` | retained (reviewer independence formalized separately) |
| N23 Solidity scorecard and exit criteria (2087–2126) | `docs/governance/quality-rubric.md` | retained under evidence-based scoring rule |
| §28 Definition of done (13662–13877) | `docs/governance/quality-rubric.md` | retained (states normalized) |
| N18 Feature maturity matrix (1719–1800) | `docs/governance/capability-maturity.md` | retained (venture no longer gated behind org milestones — see D-29) |
| N20 Documentation architecture (1930–2024) | `docs/governance/documentation-rules.md` | retained (now physically applied) |
| N21 Supersession and compatibility map (2025–2044) | `docs/governance/documentation-rules.md` | retained + V12 rows in migration map |
| §23 Documentation synchronization (13010–13033) | `docs/governance/documentation-rules.md` | retained |
| §30 Official platform and security references (13959–14090) | `docs/governance/documentation-rules.md` | retained (contains literal 'Atlassian Forge' product reference) |
| Part II preamble (2127–2130) | `docs/legacy/preserved-brainstorming-index.md` | retained |
| §1 The decision (2275–2363) | `docs/legacy/preserved-brainstorming-index.md` | retained |
| §2 Why the name Delivery Foundry (2364–2379) | `docs/legacy/preserved-brainstorming-index.md` | retained |
| §3A Operating modes (historical compendium) (2405–2536) | `docs/legacy/preserved-brainstorming-index.md` | retained (historical) |
| N19 Implementation roadmap (serialized M0–M7) (1801–1929) | `docs/legacy/preserved-brainstorming-index.md` | superseded — superseded by dual-track roadmap D-29 in delivery_foundry.md + sizing in docs/architecture/overview.md |
| §26 Rollout plan (13288–13559) | `docs/legacy/preserved-brainstorming-index.md` | superseded — superseded by dual-track roadmap D-29 |
| §27 First implementation order (13560–13661) | `docs/legacy/preserved-brainstorming-index.md` | superseded — superseded by Shared Kernel Proof + two Minimum Lovable Slices (delivery_foundry.md) |
| §20 Workflow state (flat 46-state enum) (11545–11624) | `docs/legacy/preserved-brainstorming-index.md` | superseded — superseded by canonical state model + historical mapping (docs/architecture/state-model.md) |
| §31 Document change log (14092–14293) | `CHANGELOG.md` | retained (state names normalized) |

Global transformations applied to relocated content: `Forge`→`PEC` / `forge`→`pec` (except the literal product reference "Atlassian Forge" in §30); `TEN_X_BRANCHES_READY`→`TEN_X_BRANCH_HANDOFF_READY` (old name survives only as a documented deprecated alias in the state model); `terminal_status:` completion keys→`result_code:`; stale "Version 10" self-references neutralized; 10x terminal blocks and D-13 labels rewritten to canonical `status`+`result_code` form.

New V12 documents (no V11 source): docs/autonomy/* (6), docs/security/approval-and-provenance.md, docs/security/reviewer-independence.md, docs/security/data-retention-and-privacy.md (front), docs/architecture/state-model.md (front), docs/architecture/authority-model.md (front), docs/architecture/data-consistency.md (front), docs/architecture/adr/ADR-000-build-vs-buy.md, docs/workflows/mockup-to-delivery.md, docs/operations/cost-accounting.md, docs/operations/control-plane-protection.md (front), docs/operations/observability-and-alerts.md (front), docs/providers/openai.md, docs/providers/local-models.md, root delivery_foundry.md, CHANGELOG.md front.
