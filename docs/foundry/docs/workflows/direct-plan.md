# Direct PLAN.md Execution Workflow

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative workflow.** Plan approval and provenance are governed by `../security/approval-and-provenance.md`; admission tiers by `../autonomy/admission-tiers.md`.


---

<!-- Relocated from V11: §5.12 Direct PLAN.md execution (lines 5626-6297) -->

## 5.12 Direct `PLAN.md` execution as a first-class workflow

Delivery Foundry supports three independent entry points:

```text
Mission entry
→ research and ideation
→ validation
→ specification
→ planning
→ delivery

Requirement entry
→ specification
→ planning
→ delivery

PLAN entry
→ admission
→ repository/workspace preparation
→ delivery
```

When the input is already an executable `PLAN.md`, Delivery Foundry must not repeat ideation or rewrite the plan unnecessarily.

### 5.12.1 Input detection

Accepted sources:

```text
CLI path
Telegram file upload
GitHub/GitLab/Bitbucket repository path
Jira attachment
Confluence attachment
approved artifact-store reference
API upload
```

CLI:

```bash
make plan-execute \
  PLAN=/absolute/path/to/PLAN.md \
  PROFILE=personal-github
```

Telegram:

```text
Upload PLAN.md
then:
/run-plan <file-id>
```

API concept:

```http
POST /v1/plan-executions
Content-Type: multipart/form-data

plan=@PLAN.md
profile=personal-github
```

The intake router classifies the artifact:

```text
executable PLAN
draft PLAN
specification
idea/mission
unknown document
```

It must not classify every Markdown file as an executable plan.

### 5.12.2 Plan admission, not blind execution

The admission stage verifies:

- metadata and status;
- source of truth;
- goal;
- repository declarations;
- task IDs;
- task domains;
- dependencies;
- execution waves;
- shared-file conflicts;
- acceptance/ATDD mapping;
- validation commands;
- risk classification;
- required permissions;
- secret requirements;
- integration expectations;
- branch and pull-request strategy;
- merge ordering;
- deployment mode;
- rollback expectations.

Admission result:

```text
ADMITTED
ADMITTED_WITH_REPAIRS
WAITING_FOR_COMMAND
REJECTED
```

Automatic repairs are limited to safe structural changes:

```text
derive missing wave table from explicit dependencies
split same-file tasks into sequential waves
normalize repository aliases
add missing notification nodes
add deterministic checkpoints
resolve known repository default branches
```

The system must ask before changing:

```text
business behavior
API semantics
data ownership
security assumptions
scope
deployment target
acceptance criteria
repository inclusion without confirmed evidence
```

Example report:

```text
PLAN admission: ADMITTED_WITH_REPAIRS

Plan: Unified Entitlement Delivery
Tasks: 22
Repositories: 4
Waves: 6

Repairs:
- split Wave 3 because TASK-14 and TASK-16 both modify api/routes.go
- normalized repository alias internal-api → platform-api
- added checkpoint before database migration

Commands required:
- none

Next:
repository resolution
```

### 5.12.3 Multi-repository PLAN extension

A cross-repository plan should include:

```yaml
plan:
  key: PLAN-42
  name: Unified Entitlement Delivery
  status: Ready for Implementation

repositories:
  api:
    scm: github
    url: git@github.com:acme/product-api.git
    base_branch: main
    responsibility: domain, public API, database

  web:
    scm: github
    url: git@github.com:acme/product-web.git
    base_branch: main
    responsibility: web frontend

  mcp:
    scm: github
    url: git@github.com:acme/product-mcp.git
    base_branch: main
    responsibility: MCP server

  infrastructure:
    scm: github
    url: git@github.com:acme/product-infra.git
    base_branch: main
    responsibility: preview and production delivery

branching:
  strategy: per-repository-group
  naming: "feature/{{ plan.key }}-{{ group.id }}-{{ slug }}"

change_set:
  strategy: linked-pull-requests
  merge_order:
    - api
    - mcp
    - web
    - infrastructure

deployment:
  production:
    mode: auto
```

Every task references a repository alias:

```yaml
- id: TASK-01
  repository: api
  group: G1
  wave: 1
  goal: Freeze the public API contract
  files:
    - docs/openapi.yaml
  acceptance:
    - AT-01
  validation:
    - make contract-check

- id: TASK-02
  repository: api
  group: G2
  wave: 2
  depends_on: [TASK-01]
  goal: Implement the entitlement endpoint

- id: TASK-03
  repository: web
  group: G3
  wave: 2
  depends_on: [TASK-01]
  goal: Implement the frontend API client

- id: TASK-04
  repository: mcp
  group: G4
  wave: 2
  depends_on: [TASK-01]
  goal: Implement the MCP adapter
```

### 5.12.4 Repository resolution

Resolution order:

```text
1. explicit PLAN repository URL
2. active profile repository catalog
3. linked source-of-truth metadata
4. work-item mappings
5. dependency/service catalog
6. confirmed repository-discovery proposal
```

The resolver checks:

- repository is allowed by the profile;
- credentials can read it;
- base branch exists;
- branch restrictions are understood;
- repository is not archived;
- open conflicting changes are visible;
- required LFS/submodules are supported;
- bootstrap commands are known;
- default branch is not assumed from memory.

A plan does not grant repository access.

### 5.12.5 Missing repository discovery

Delivery Foundry may detect a likely missing repository from:

- OpenAPI consumers;
- generated clients;
- package/module imports;
- deployment manifests;
- CODEOWNERS;
- service catalog;
- CI dependencies;
- database or event contracts.

Default policy:

```yaml
repository_discovery:
  auto_add_if:
    repository_in_allowlist: true
    dependency_evidence: confirmed
    required_by_acceptance: true
    write_scope_known: true

  otherwise:
    mode: command
    channel: telegram
```

Notification:

```text
🔎 Possible missing repository

Plan: PLAN-42
Task: TASK-08

Evidence:
notification-service owns the client imported by identity-settings.

Suggested action:
/add-repository flow-123 notification-service
/reject-repository flow-123 notification-service
```

### 5.12.6 Workspace Manager

The Workspace Manager is deterministic kernel infrastructure.

It creates:

```text
workspaces/flows/flow-123/
├── PLAN.md
├── plan-admission.json
├── repository-manifest.yaml
├── change-set.yaml
├── repositories/
│   ├── product-api/
│   ├── product-web/
│   ├── product-mcp/
│   └── product-infra/
├── worktrees/
│   ├── api/
│   │   ├── G1-contract/
│   │   └── G2-endpoint/
│   ├── web/
│   │   └── G3-client/
│   ├── mcp/
│   │   └── G4-adapter/
│   └── infrastructure/
│       └── G5-delivery/
├── integration/
├── artifacts/
├── checkpoints/
└── state/
```

Preparation flow:

```text
resolve repository
→ update or create read-only mirror
→ fetch required base branch
→ verify exact base SHA
→ create isolated worktree
→ create branch
→ bootstrap
→ baseline build/test
→ reserve workspace lease
```

The system never lets two agents own the same worktree simultaneously.

### 5.12.7 Clone and mirror strategy

Use reusable local mirrors:

```text
workspaces/mirrors/<scm>/<owner>/<repository>.git
```

For a new flow:

```text
fetch mirror
→ pin base SHA
→ create disposable worktree
```

Benefits:

- faster repeated setup;
- less network traffic;
- consistent base revisions;
- safe disposable execution;
- easy recovery after agent/session failure.

Mirrors are read-only to agents. Pushes occur only through authorized SCM operations.

### 5.12.8 Branch strategies

Supported:

| Strategy | Behavior |
|---|---|
| `per-task` | One branch and potential PR for every task |
| `per-group` | One branch for related tasks in one domain/group |
| `per-repository` | One branch per affected repository |
| `per-repository-group` | One branch per group inside each repository |
| `single-change-set` | Coordinated branches across repositories under one change-set ID |
| `existing-branch` | Resume work from explicitly approved existing branches |

Default:

```yaml
branching:
  strategy: per-repository-group
```

The planner may combine tiny tasks to avoid generating dozens of trivial branches and PRs.

### 5.12.9 Cross-repository execution waves

Example:

```text
Wave 1
└── API contract freeze

Wave 2
├── API domain implementation
├── Web client against frozen contract
├── MCP schema and adapter
└── Infrastructure preview skeleton

Wave 3
├── API handler and integration
├── Web workflow integration
├── MCP/API contract verification
└── Preview deployment

Wave 4
└── Cross-repository end-to-end validation
```

Tasks may run in parallel only when:

- dependencies permit it;
- file sets do not overlap;
- workspace leases are separate;
- shared contracts are frozen;
- capacity is available.

### 5.12.10 Contract-first coordination

Cross-repository consumers must depend on explicit contract artifacts:

```text
OpenAPI
AsyncAPI
protobuf
JSON Schema
MCP tool schema
event schema
database migration contract
environment/configuration contract
container image version
```

A consumer does not wait for the full producer implementation when a frozen contract and mock are sufficient.

### 5.12.11 Integration environment

After repository-local verification:

```text
selected repository revisions
→ integration manifest
→ Docker Compose / Kubernetes namespace / approved test environment
→ database migrations
→ service startup
→ cross-repository contract tests
→ primary user journey
→ failure and rollback tests
```

Example manifest:

```yaml
integration:
  id: integration-flow-123

  components:
    api:
      repository: api
      revision: commit-api
      image: api:flow-123

    web:
      repository: web
      revision: commit-web
      image: web:flow-123

    mcp:
      repository: mcp
      revision: commit-mcp
      image: mcp:flow-123

  tests:
    - make contract-test
    - make integration-test
    - make e2e-primary
```

### 5.12.12 Cross-repository change set

```yaml
change_set:
  id: CHANGE-42
  plan: PLAN-42
  status: integrating

  pull_requests:
    api:
      url: https://github.com/acme/product-api/pull/84
      state: ready
      revision: commit-api

    web:
      url: https://github.com/acme/product-web/pull/55
      state: ready
      revision: commit-web

    mcp:
      url: https://github.com/acme/product-mcp/pull/12
      state: ci-running
      revision: commit-mcp

    infrastructure:
      url: https://github.com/acme/product-infra/pull/31
      state: waiting-for-api-image

  merge_order:
    - api
    - mcp
    - web
    - infrastructure

  rollback_order:
    - infrastructure
    - web
    - mcp
    - api
```

The change set is the unit of orchestration even though SCMs expose separate pull requests.

### 5.12.13 Pull-request creation

For every repository, the Foundry can:

- push the authorized branch;
- open PR/MR;
- link `PLAN.md`;
- link related PRs;
- attach verification evidence;
- record acceptance IDs;
- request reviewers;
- monitor CI;
- perform bounded remediation;
- update the change-set manifest;
- notify every transition.

### 5.12.14 Merge and deployment

Default profile behavior:

```yaml
steps:
  merge:
    mode: auto

deployment:
  preview:
    mode: auto
  staging:
    mode: auto
  production:
    mode: auto
```

Command-controlled production:

```yaml
deployment:
  production:
    mode: command
    command:
      channel: telegram
      timeout: null
```

Merge order, deployment order, and rollback order come from the change-set contract.

### 5.12.15 Restart and resume

A provider limit, machine restart, failed sandbox, or new provider session resumes one task—not the entire multi-repository plan.

Checkpoint contains:

```text
plan and admission version
repository base and current SHAs
branch/worktree leases
accepted task evidence
current wave
change-set state
integration environment state
CI and PR state
deployment state
next action
```

Recovery:

```text
load checkpoint
→ fence stale workers
→ verify mirrors and worktrees
→ recreate missing sandbox/session
→ validate accepted evidence
→ resume unfinished task/wave
```

### 5.12.16 Notification contract

Every stage emits notifications:

```text
PLAN uploaded
PLAN classified
admission started/completed
repository resolved/rejected
mirror fetched
branch/worktree created
baseline passed/failed
wave scheduled/started/completed
task progress/checkpoint/retry
contract frozen
integration environment created
PR opened/updated
CI started/failed/passed
merge scheduled/completed
deployment started/completed/rolled back
workflow completed/proven blocked
```

Telegram batching rules still apply. Every event is durable even when multiple transitions appear in one digest.

### 5.12.17 Completion definition

A direct-plan workflow is `COMPLETED` only when:

- the plan passed admission;
- all required repositories were resolved;
- all required tasks and acceptance criteria passed;
- all repository-local verification passed;
- cross-repository contracts passed;
- integration tests passed;
- linked change requests satisfy policy;
- configured merge mode completed;
- configured deployment mode completed;
- health/stabilization passed;
- documentation and evidence were persisted;
- no required task remains hidden in a failed or waiting state.

Code generation alone is not completion.





---

<!-- Relocated from V11: §13.8 Worked scenario: four-repository PLAN (lines 8592-8971) -->

## 13.8 Worked scenario: uploaded `PLAN.md` spanning four repositories

See **D-23 — Direct PLAN multi-repository execution** for the complete process flow.

### Scenario

The operator has already completed ideation and planning.

They upload:

```text
PLAN.md — Unified Customer Notification Flow
```

The plan spans:

```text
web-console
platform-api
notification-service
infrastructure
```

The operator does not ask Delivery Foundry to brainstorm or generate another product idea.

### Step 1 — Intake

CLI:

```bash
make plan-execute \
  PLAN=./PLAN.md \
  PROFILE=personal-github
```

Telegram:

```text
Upload PLAN.md
/run-plan file-789
```

Notification:

```text
📥 PLAN received

File: PLAN.md
Classification: executable plan
Detected tasks: 18
Detected repositories: 4

Ideation: skipped
Market validation: skipped
Plan admission: starting
```

### Step 2 — Admission

The Foundry checks:

```text
status
source of truth
repository aliases
task dependencies
execution waves
shared files
acceptance criteria
validation commands
risk
deployment mode
```

Assume it finds:

```text
TASK-06 and TASK-08 both modify api/routes.go in Wave 2
```

This is a structural conflict. It automatically separates them into Wave 2 and Wave 3 and records the repair.

Notification:

```text
🧭 PLAN admission complete

Status: ADMITTED_WITH_REPAIRS
Tasks: 18
Repositories: 4
Waves: 6

Automatic repair:
- moved TASK-08 to Wave 3 due to shared-file conflict

No command required.
```

### Step 3 — Repository resolution

The SCM adapter resolves:

```text
web-console           → github.com/acme/web-console
platform-api          → github.com/acme/platform-api
notification-service  → github.com/acme/notification-service
infrastructure        → github.com/acme/infrastructure
```

For each repository:

- validate allowlist;
- validate credentials;
- fetch base branch;
- record exact SHA;
- inspect branch rules;
- detect open conflicting PRs;
- load repository Make contract.

Notification digest:

```text
📦 Repository resolution

✅ web-console
✅ platform-api
✅ notification-service
✅ infrastructure

Base branches pinned.
No conflicting change set detected.
Workspace preparation starting.
```

### Step 4 — Mirrors, worktrees, and branches

Workspace:

```text
workspaces/flows/flow-789/
├── repositories/
│   ├── web-console/
│   ├── platform-api/
│   ├── notification-service/
│   └── infrastructure/
└── worktrees/
    ├── platform-api/G1-contract/
    ├── platform-api/G2-endpoint/
    ├── web-console/G3-ui/
    ├── notification-service/G4-email/
    └── infrastructure/G5-delivery/
```

Branches:

```text
feature/PLAN-789-G1-contract
feature/PLAN-789-G2-api
feature/PLAN-789-G3-web
feature/PLAN-789-G4-notification
feature/PLAN-789-G5-infrastructure
```

Every worktree runs bootstrap and baseline checks before an agent can edit it.

### Step 5 — Contract wave

Wave 1 freezes:

```text
OpenAPI request/response
notification payload schema
environment-variable contract
image/version naming
```

Consumers can start after the contracts pass, even while backend implementation continues later.

### Step 6 — Parallel implementation

Wave 2:

```text
Codex
→ platform-api domain implementation

Cursor
→ web-console client and UI using frozen OpenAPI

Claude/implementation agent
→ notification-service adapter

DevOps agent
→ infrastructure preview configuration
```

The kernel owns scheduling and authoritative state. The Plan Execution Coordinator proposes waves and bounded dispatch inside its admitted plan; agents only own bounded worktrees. (Authority table: docs/architecture/authority-model.md.)

Notifications are emitted for:

```text
task scheduled
task started
progress
test result
checkpoint
retry
completion
```

Telegram may represent multiple progress events through edited messages or a digest.

### Step 7 — Provider/session interruption

Suppose the frontend executor reaches its provider usage limit.

The Foundry:

```text
checkpoints web worktree
→ stores accepted tests and current patch
→ releases the provider session
→ marks WAITING_FOR_SUBSCRIPTION_RESET
→ schedules wake_at
→ continues independent backend and notification tasks
```

It does not restart the whole plan.

After reset:

```text
new provider session
→ load provider-neutral task packet
→ verify repository/worktree state
→ continue frontend task
```

### Step 8 — Repository-local verification

Each repository executes its own deterministic contract:

```bash
make bootstrap
make lint
make test-unit
make test-integration
make build
make verify
```

Repositories without standard targets use an approved repository manifest.

### Step 9 — Integration

Selected revisions are assembled into one integration environment:

```text
web-console
+ platform-api
+ notification-service
+ infrastructure
→ cross-repository test environment
```

The Foundry verifies:

- web-to-API flow;
- API-to-notification flow;
- schema compatibility;
- authentication;
- retry and idempotency;
- failure recovery;
- deployment configuration.

### Step 10 — Linked pull requests

The Foundry opens:

```text
platform-api PR #84
web-console PR #55
notification-service PR #21
infrastructure PR #31
```

All are linked to:

```text
CHANGE-789
```

Telegram:

```text
🔗 Cross-repository change set

API: READY
Web: CI RUNNING
Notification: READY
Infrastructure: WAITING FOR WEB IMAGE

Overall: INTEGRATING
```

### Step 11 — Merge and deployment

With defaults:

```yaml
steps:
  merge:
    mode: auto

deployment:
  production:
    mode: auto
```

The Foundry merges in the declared order when policy and CI pass, deploys automatically, verifies health, and rolls back if deterministic health checks fail.

To require Telegram:

```yaml
deployment:
  production:
    mode: command
```

Then the Foundry reaches:

```text
WAITING_FOR_COMMAND
```

and sends:

```text
/deploy flow-789
/details flow-789
/cancel flow-789
```

### Step 12 — Completion

The workflow finishes only after:

```text
18/18 tasks accepted
all repository checks passed
integration passed
4 linked PRs satisfy merge policy
deployment completed
stabilization passed
evidence stored
```

Final notification:

```text
✅ PLAN execution completed

Plan: PLAN-789
Repositories: 4
Tasks: 18/18
PRs: 4
Deployment: healthy
Rollbacks: 0
Total provider restarts: 1
Final change set: CHANGE-789
```

### Scenario guarantee

This scenario confirms the intended operating model:

> A user can provide an existing executable `PLAN.md`, bypass ideation and planning, and let Delivery Foundry resolve multiple repositories, clone or refresh them, create isolated branches and worktrees, execute dependency-aware tasks in parallel, survive provider/session interruptions, integrate the repositories, create linked pull requests, merge and deploy according to configuration, and notify every process until completion or a proven blocker.



