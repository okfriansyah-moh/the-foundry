# Venture Loop (Personal Autonomous Venture Track)

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative workflow.** The venture loop is a first-class product track, not an extension. Its governing contracts: `autonomy/mission-contract.md` (targets, exit semantics), `autonomy/mission-setup-ceremony.md` (readiness), `autonomy/admission-tiers.md` (self-generated improvement plans auto-admit inside the envelope), `autonomy/personal-venture-profile.md` (auto-deployment), `autonomy/cumulative-drift-governance.md` (self-adaptation), `operations/cost-accounting.md` (mission economics).

The preserved step-by-step venture workflow follows.


---

<!-- Relocated from V11: §14 Personal venture workflow (lines 9323-10239) -->

## 14. Personal venture workflow: step by step

Every step below emits `scheduled`, `started`, `progress`, `checkpointed`, `waiting/retrying`, and `completed/failed` events according to the notification policy.

### Step 1 — Activate personal profile

```bash
make profile-use PROFILE=personal-github
make doctor
```

Doctor validates:

- GitHub access.
- Telegram bot.
- OpenHands.
- Claude Code.
- Codex.
- Cursor.
- Copilot.
- OpenCode and 9Router fallback.
- Deployment provider.
- Workspace isolation.
- Personal repository allowlist.

OpenHands and 9Router in this workflow are ADR-deferred pluggable externals, not core requirements; see
`../architecture/adr/ADR-001-openhands-9router.md`.

### Step 2 — Submit the mission

```text
Build profitable AI SaaS businesses for solo founders.
Budget: $500/month.
Target: $10,000 ARR per product.
Prefer B2B.
Avoid regulated industries.
```

The system stores a `VentureProgram`.

### Step 3 — Discover opportunities

The daily loop:

```text
public research
→ evidence collection
→ ten candidates
→ deduplication
→ initial rejection
```

### Step 4 — Score independently

```text
researcher generates
→ validator checks evidence
→ skeptic tries to reject
→ scorer ranks
```

### Step 5 — Validate top three

For each candidate:

- competitor map;
- customer language;
- pricing;
- distribution;
- landing page;
- waitlist;
- small outreach test;
- technical feasibility;
- unit economics.

Do not build three complete products.

### Step 6 — Select one candidate

Automatic only when thresholds pass.

Otherwise, run another experiment or reject all candidates.

### Step 7 — Create repository

```bash
make repo-create \
  NAME=api-changelog-assistant \
  TEMPLATE=delivery-foundry-product-template
```

The GitHub adapter:

- creates the repository;
- initializes branch rules;
- installs webhooks;
- selects GitHub Actions templates;
- pushes the initial commit;
- creates initial issues or milestones.

### Step 8 — Create product specification

Claude Code gathers:

- venture program;
- validation artifacts;
- selected customer;
- pricing;
- constraints.

It writes `SPEC.md`.

A different provider reviews it.

### Step 9 — Plan

Create atomic tasks with:

- acceptance criteria;
- allowed paths;
- verification commands;
- risk level;
- preferred agent;
- reviewer.

### Step 10 — Build

OpenHands dispatches isolated tasks to native subscription agents.

ADR-001 defers OpenHands as an optional pluggable external; this dispatch example is not a core runtime mandate
(`../architecture/adr/ADR-001-openhands-9router.md`).

Routing example:

```text
Architecture → Claude Code
Backend → Codex
Frontend → Cursor
PR review → Copilot
Fallback → OpenCode through 9Router
```

ADR-001 also defers 9Router as an optional pluggable external; fallback remains in-allowlist unless a future profile
explicitly approves such an adapter (`../architecture/adr/ADR-001-openhands-9router.md`).

### Step 11 — Verify

Every task runs deterministic product Make targets:

```bash
make lint
make typecheck
make test-unit
make test-integration
make test-contract
make test-e2e
make security
make build
```

### Step 12 — Create pull request

The GitHub adapter creates a pull request and links evidence.

### Step 13 — Preview

The deployment adapter creates a preview.

A browser agent verifies:

- onboarding;
- primary workflow;
- mobile;
- errors;
- billing sandbox;
- API;
- MCP.

### Step 14 — Commercial-readiness gate

The system confirms:

- landing page;
- pricing;
- signup;
- onboarding;
- primary value;
- billing;
- cancellation;
- privacy;
- terms;
- support;
- analytics.

### Step 15 — Ship

Production remains a hard gate initially.

```text
approval
→ deploy
→ smoke tests
→ stabilization window
→ mark shipped
```

### Step 16 — Growth loop

Daily or weekly:

```text
analytics
→ customer behavior
→ revenue
→ support feedback
→ new hypotheses
→ improve, pivot, or kill
```

---


### 14.1 Complete worked example: autonomous B2B SaaS mission

Initial operator instruction:

```text
Build profitable AI SaaS businesses for solo founders.

Budget: $500/month
Target: $10,000 ARR per successful product
Market: B2B
Avoid: regulated industries

Required product surfaces:
- Web frontend
- Public API
- MCP server
```

This message is a **portfolio mission**, not an instruction to immediately build three applications.

The system first creates these persistent artifacts:

```text
ventures/solo-founder-b2b/
├── VENTURE_PROGRAM.md
├── SCORING_POLICY.yaml
├── BUDGET_POLICY.yaml
├── APPROVAL_POLICY.yaml
├── DISTRIBUTION_POLICY.yaml
├── PRODUCT_CONTRACT.yaml
├── state/
│   └── PORTFOLIO-STATE.json
├── research/
├── validations/
└── products/
```

#### Generated venture program

```yaml
mission:
  objective: build-profitable-ai-saas
  target_customer: solo-founders
  market: b2b
  target_arr_usd: 10000
  monthly_budget_usd: 500

constraints:
  regulated_industries: excluded
  maximum_active_builds: 1
  maximum_parallel_validations: 3
  ideas_per_cycle: 10

required_surfaces:
  web_frontend: required
  public_api: required_after_selection
  mcp_server: required_after-selection

commercial_requirements:
  pricing_page: required
  billing: required_before-production
  analytics: required
  onboarding: required
  support_path: required

autonomy:
  research: automatic
  scoring: automatic
  lightweight_validation: automatic-within-budget
  product_selection: soft-gate
  paid-spend: hard-gate
  public-production-release: hard-gate
```

#### Phase A — Daily opportunity loop

A scheduled workflow wakes up once per day:

```text
market signal collection
→ complaint and workflow extraction
→ competitor and price discovery
→ duplicate removal
→ 10 evidence-backed ideas
→ independent scoring
→ Telegram digest
```

Each idea must contain:

```json
{
  "id": "idea-2026-0718-04",
  "problem": "Solo SaaS founders manually reconcile API changelogs",
  "target_customer": "B2B SaaS founders maintaining multiple integrations",
  "current_workaround": "Email, bookmarks, GitHub releases, and spreadsheets",
  "pain_evidence": [],
  "competitors": [],
  "pricing_hypothesis": "$19-$79/month",
  "reachable_channels": [
    "GitHub Marketplace",
    "developer communities",
    "integration partner directories"
  ],
  "mvp_hypothesis": "Track dependencies and generate prioritized migration tasks",
  "risks": [],
  "estimated_validation_cost_usd": 14
}
```

The system must reject ideas with no evidence, no reachable customer, weak gross margins, platform-policy risk, or a value proposition that is only “uses AI.”

Telegram:

```text
Opportunity cycle complete

10 generated
6 passed evidence threshold
3 selected for deep validation
Research cost: $3.12

No action required.
```

#### Phase B — Independent scoring

Use separate roles:

```text
research agent
    ↓
evidence validator
    ↓
skeptic / kill agent
    ↓
portfolio scorer
```

The generator may not assign the final score to its own ideas.

Example rubric:

```text
25% pain severity
20% willingness-to-pay evidence
15% reachable distribution
15% founder fit
10% speed to MVP
10% recurring revenue potential
 5% defensibility
minus risk and cost penalties
```

#### Phase C — Validate the top three without building three products

For each candidate, create:

```text
validations/<idea-id>/
├── MARKET.md
├── CUSTOMER-LANGUAGE.md
├── COMPETITORS.md
├── PRICING.md
├── DISTRIBUTION.md
├── UNIT-ECONOMICS.md
├── RISKS.md
├── landing-page/
├── experiment-plan.yaml
└── VALIDATION-REPORT.json
```

Allowed validation work:

- landing page;
- waitlist;
- pricing test;
- clickable prototype;
- five to twenty direct outreach messages;
- one bounded traffic experiment;
- customer interviews;
- technical spike where feasibility is genuinely uncertain.

Disallowed before product selection:

- full authentication;
- production billing;
- complete public API;
- complete MCP server;
- complex infrastructure;
- three separate production applications.

#### Phase D — Select one winner

Automatic selection requires:

```yaml
minimum_total_score: 75
minimum_distribution_score: 65
minimum_payment_evidence_score: 60
must_have_real_validation_signal: true
maximum_mvp_budget_usd: 150
maximum_active_builds: 1
```

Valid outcomes:

```text
one passes    → select it
multiple pass → select the strongest risk-adjusted candidate
none pass     → build nothing and start another research cycle
unclear       → run one more bounded experiment
```

“Build nothing” is a successful decision when evidence is weak.

#### Phase E — Create the product repository

Assume the selected product is **API Changelog Assistant**.

```bash
make repo-create \
  PROFILE=personal-github \
  NAME=api-changelog-assistant \
  TEMPLATE=delivery-foundry-product-template
```

Generated repository:

```text
api-changelog-assistant/
├── .foundry/
│   ├── context/
│   │   ├── PRODUCT_PROGRAM.md
│   │   ├── VALIDATION-REPORT.json
│   │   └── SPEC.md
│   ├── skills/
│   │   └── enabled.yaml
│   └── state/
│
├── apps/
│   ├── web/
│   ├── api/
│   └── mcp/
│
├── packages/
│   ├── domain/
│   ├── database/
│   ├── auth/
│   ├── billing/
│   ├── analytics/
│   └── testing/
│
├── docs/
│   ├── PLAN.md
│   ├── review-report.md
│   ├── verification-report.md
│   └── release/
│
├── AGENTS.md
├── CLAUDE.md
└── Makefile
```

#### Phase F — Product contract

All three surfaces share one domain implementation:

```text
domain service
    ↓
versioned public API + OpenAPI
    ├── web frontend
    └── MCP adapter
```

Required product surface rules:

**Web frontend**

- responsive landing page;
- sign-up and authentication;
- onboarding;
- primary workflow;
- empty, loading, success, and error states;
- pricing and upgrade;
- account, cancellation, export, and deletion;
- support/contact path.

**Public API**

- versioned paths;
- OpenAPI specification;
- authentication;
- request validation;
- idempotency where relevant;
- pagination and bounded queries;
- rate limits;
- stable error envelope;
- API documentation and examples.

**MCP server**

- wraps the same public domain/API capability;
- explicit tool schemas;
- least-privilege credentials;
- bounded tool outputs;
- no separate business-logic implementation;
- installation and usage documentation.

#### Phase G — Specification

The planning intake creates `SPEC.md` with:

```text
problem and target user
business outcome
user journeys
functional requirements
non-functional requirements
frontend contract
API contract
MCP contract
data model
billing
analytics
security
deployment
acceptance criteria
out-of-scope
rollback
```

A separate agent reviews the specification before planning.

#### Phase H — PLAN.md

The planning agent loads:

```text
guardrails
stop-slop
planning
harness engineering
canonical PLAN references
```

It generates:

```text
dependency graph
execution waves
domain-scoped task groups
task-to-ATDD mapping
allowed files
validation commands
risk level
agent routing
handoff format
```

Example wave model:

```text
Wave 1
- repository and CI contract
- OpenAPI contract
- design tokens and frontend shell

Wave 2
- backend domain and repository
- frontend mocked onboarding
- MCP schema design

Wave 3
- API handlers
- frontend API client
- MCP adapter

Wave 4
- billing and usage limits
- end-to-end primary journey
- documentation

Wave 5
- independent review
- verification
- remediation
- release evidence
```

Tasks in one wave must not share files or depend on each other.

#### Phase I — PEC execution

```bash
make pec-run \
  PROFILE=personal-github \
  REPOSITORY=workspaces/products/api-changelog-assistant \
  PLAN=docs/PLAN.md
```

PEC:

1. validates the plan;
2. records the starting revision;
3. dispatches every independent wave task concurrently;
4. routes backend, frontend, review, verification, security, and DevOps work;
5. checks each returned claim against acceptance and command evidence;
6. retries a failed task at most twice;
7. prevents the next wave from starting until the current wave passes;
8. writes `ORCHESTRATION-STATE.md`;
9. runs independent review and verification;
10. resolves the configured deployment mode;
11. emits a notification for every wave, task, checkpoint, retry, wait, review, and deployment transition.

#### Phase J — Agent routing

```text
product and architecture reasoning → Claude Code
backend and repository changes     → Codex
frontend and browser refinement    → Cursor
PR review and documentation        → Copilot
provider-independent fallback      → OpenCode through 9Router
```

The 9Router fallback line is retained as a legacy routing example only; ADR-001 defers it as an optional pluggable
external (`../architecture/adr/ADR-001-openhands-9router.md`).

Routing remains task-level. A healthy task does not switch models halfway through execution.

#### Phase K — Review and verification

The reviewer checks seven pillars:

```text
correctness
security
performance
maintainability
quality gate
engineering enhancements
conventions and compliance
```

Evidence labels:

```text
Confirmed
Likely
Unverified
```

The reviewer must not fabricate Sonar, coverage, or test results.

Verification selects tests based on risk:

```text
unit        → pure logic
integration → database and service boundaries
contract    → public API and MCP schema
UI          → components
E2E         → primary user journeys only
security    → auth, tenant isolation, injection, secret handling
performance → bounded endpoints and expensive operations
```

The testing pyramid remains the default, not a rigid quota:

```text
approximately 70% unit
20–25% integration
5–10% E2E
```

#### Phase L — Frontend inspection loop

```text
preview deploy
→ browser agent inspects real application
→ capture screenshots and console/network evidence
→ create bounded defect tasks
→ remediate
→ redeploy
→ repeat until measurable thresholds pass
```

Required journeys:

```text
landing page
signup
onboarding
primary workflow
failure recovery
upgrade
billing sandbox
account settings
mobile
tablet
desktop
```

“Perfect” is not a stop condition.

Ready-to-sell means:

1. a customer understands the value;
2. can sign up;
3. completes onboarding;
4. receives the promised result;
5. can pay;
6. understands limits;
7. can cancel or export;
8. can obtain support.

#### Phase M — Deployment-mode resolution and shipping

Default:

```yaml
deployment:
  production:
    mode: auto
```

Automatic flow:

```text
release candidate ready
→ send release-ready notification
→ deterministic preflight
→ verify rollback asset
→ deploy automatically
→ send deployment-started notification
→ smoke/API/MCP/billing verification
→ stabilization window
→ send SHIPPED or ROLLED_BACK notification
```

Example Telegram message:

```text
🚀 Automatic deployment starting

Product: API Changelog Assistant
Workflow: flow-123
Version: v0.1.0

Passed:
✅ web primary journey
✅ API contract
✅ MCP contract
✅ billing sandbox
✅ security review
✅ rollback rehearsal

Mode: AUTO
Starting in 30 seconds.
Use /pause flow-123 to stop before commit.
```

Command mode:

```yaml
deployment:
  production:
    mode: command
    command:
      channel: telegram
      timeout: null
```

Flow:

```text
release candidate ready
→ enter WAITING_FOR_COMMAND
→ send Telegram release summary
→ remind according to policy
→ operator sends /deploy flow-123
→ verify identity, nonce, state, and profile authority
→ deploy
```

Example:

```text
🚦 Deployment command required

Product: API Changelog Assistant
Workflow: flow-123
Version: v0.1.0

/deploy flow-123
/cancel flow-123
/details flow-123

No timeout. The workflow remains safely checkpointed.
```

The profile may override auto deployment for environments where security, compliance, legal, spending, or organization policy requires command or human approval.

#### Phase N — Post-launch autonomous growth loop

The product wakes up on a schedule and reads real signals:

```text
traffic
activation
primary-action completion
retention
support feedback
conversion
revenue
inference and infrastructure cost
```

It then creates hypotheses:

```text
improve onboarding
refine positioning
add a high-demand integration
remove unused features
adjust pricing
publish targeted content
pause or kill the product
```

The system may automatically implement low-risk improvements, but spending, legal changes, billing changes, mass outreach, and production-impacting operations remain hard gates.


#### Phase N.1 — LLM capability plan for this product

The optimizer selects capabilities by phase.

| Phase | Recommended capability envelope |
|---|---|
| Daily idea generation | Batch processing, cached venture mission, medium effort |
| Deep market research | Web search/fetch with citations and dynamic filtering; code execution for comparisons |
| Product selection | Structured output, high effort, independent skeptic |
| Architecture | Adaptive thinking at xhigh, orchestration mode, optional advisor |
| PLAN generation | Cached policies and skills, high effort, structured plan validation |
| PEC implementation | High effort, strict tools, tool search, compaction for long waves |
| Bulk repository analysis | Programmatic tool calling to filter results before context |
| Browser refinement | Native browser/computer tool through approved sandbox |
| Review | xhigh effort, structured findings, independent context |
| Regression evaluation | Message Batch and prompt caching |
| Incident or rollback | Fast mode only when approved, strict tools, minimal context |
| Long-term product memory | Foundry memory source of truth with provider memory projection |

Example context strategy:

```text
stable cached prefix
- security kernel reference
- profile policy
- agent definition
- relevant skill manifests
- product program
- frozen API contract

dynamic suffix
- current PLAN task
- changed files
- test result summary
- current blocker
```

When the PEC session grows:

```text
tool search
→ avoid loading unused provider tools

programmatic tool calling
→ compress bulk repository/Jira operations

context editing
→ remove stale successful tool output

server compaction
→ preserve objective, state, evidence, budget, and next action
```


#### Phase O — Operator involvement

After the system is mature:

```text
Daily:
- read one Telegram exception digest
- answer bounded questions

Weekly:
- review cost, validation evidence, users, revenue, blocked work
- continue, pivot, or kill

Monthly:
- adjust venture mission, budget, and portfolio policy
```

The target is not zero humans. The target is to reserve humans for business judgment, ambiguity, money, legal risk, and exceptions.


