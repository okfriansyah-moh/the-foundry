# 10x Shared-Branch Direct-Push Workflow (Track B)

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative workflow.** Terminal result: `status: SUCCEEDED, result_code: TEN_X_BRANCH_HANDOFF_READY` — a successful stop boundary, never a failure. Organization plan approval requires strong-auth provenance (`../security/approval-and-provenance.md`).


---

<!-- Relocated from V11: §5.13 10x shared-branch direct-push execution (lines 6298-6749) -->

## 5.13 10x shared-branch direct-push execution

This is a specialized direct-PLAN workflow for coordinated delivery sessions where all contributors work against an initiative branch such as `10x-branch`.

It deliberately changes the delivery boundary:

```text
Normal direct-plan workflow
→ branch/worktree
→ PR
→ merge
→ deploy

10x direct-push workflow
→ local isolated worktree
→ accepted commit
→ serialized push to existing shared 10x branch
→ branch readiness
→ stop
```

### 5.13.1 Source process mapped into Foundry

The real 10x operating model uses:

- reviewed QA artifacts before implementation;
- `PLAN.md` as the shared execution contract;
- explicit dependency groups;
- parallel work where safe;
- bounded backend, frontend, reviewer, verification, and PEC agents;
- multiple repositories working on `10x-branch`;
- continuous testing, lint, quality review, and cross-service integration;
- direct incremental pushes to the initiative branch during implementation.

The Foundry profile starts from the already approved `PLAN.md`. It verifies that upstream QA/ATDD references exist but does not regenerate them unless the plan is rejected as non-executable.

### 5.13.2 Profile configuration

```yaml
api_version: foundry.profile/v1
kind: Profile

metadata:
  name: organization-10x-direct-push

mode: plan-execution
workflow: ten-x-direct-push

plan:
  required_status:
    - Ready for Implementation
    - Approved
  require_acceptance_references: true
  require_repository_mapping: true

branch_delivery:
  mode: direct-push
  branch_source: plan-or-profile
  default_target_branch: 10x-branch
  create_target_if_missing: false
  create_remote_task_branches: false

  push_cadence: after-accepted-task
  serialize_per_repository: true
  normal_push_only: true
  force_push: forbidden
  require_remote_refresh: true
  require_post-rebase-verification: true

pull_requests:
  enabled: false
  create_draft: false
  create_final: false

merge:
  enabled: false

deployment:
  preview:
    mode: disabled
  staging:
    mode: disabled
  production:
    mode: disabled

integration:
  mode: local-or-existing-test-endpoint
  create_remote_environment: false

review:
  per_task: focused
  per_wave: independent-ai-review
  final_branch_review: required

completion:
  result_code: TEN_X_BRANCH_HANDOFF_READY
  require:
    - every-required-task-accepted
    - every-accepted-commit-pushed
    - branch-heads-recorded
    - repository-local-checks-passed
    - cross-repository-contracts-passed
    - configured-local-integration-passed
    - final-branch-review-passed
```

### 5.13.3 Repository mapping

The profile may declare the same working branch for multiple repositories:

```yaml
repositories:
  acme-tools-dashboard:
    url: ssh://bitbucket.example/acme-tools-dashboard.git
    base_branch: 10x-branch
    target_branch: 10x-branch

  acme-internal-tools-service:
    url: ssh://bitbucket.example/acme-internal-tools-service.git
    base_branch: 10x-branch
    target_branch: 10x-branch

  acme-mail-service:
    url: ssh://bitbucket.example/acme-mail-service.git
    base_branch: 10x-branch
    target_branch: 10x-branch

  acme-company-settings:
    url: ssh://bitbucket.example/acme-company-settings.git
    base_branch: 10x-branch
    target_branch: 10x-branch
```

The branch name may also be initiative-specific:

```yaml
branch_delivery:
  default_target_branch: "10x/{{ plan.key }}"
```

The existing branch must be confirmed during admission. The system does not silently fall back to `main` or `master`.

### 5.13.4 Local isolation with shared remote branches

Agents must not edit one shared checkout.

Each task or group still receives an isolated local worktree:

```text
workspaces/flows/flow-10x/
├── repositories/
│   ├── acme-tools-dashboard/
│   ├── acme-internal-tools-service/
│   ├── acme-mail-service/
│   └── acme-company-settings/
└── worktrees/
    ├── acme-tools-dashboard/G5-ui/
    ├── acme-internal-tools-service/G2-license/
    ├── acme-mail-service/G4-email/
    └── acme-company-settings/G7-invite/
```

These are local execution branches/worktrees only. They are not pushed as remote task branches.

### 5.13.5 Branch Integrator

Parallel implementation is safe; parallel writes to the same remote branch are not.

The deterministic Branch Integrator owns all pushes:

```text
task agent finishes
→ local validation passes
→ commit signed/created
→ push request enters repository queue
→ Branch Integrator acquires repository lease
→ fetch origin/10x-branch
→ compare expected and current remote SHA
→ replay accepted commit onto latest branch
→ resolve or escalate conflicts
→ rerun focused checks
→ normal fast-forward push
→ record push receipt
→ release lease
```

Rules:

- agents do not push directly;
- one integration lease per repository;
- never use `--force`;
- never use `--force-with-lease` on the shared 10x branch;
- non-fast-forward causes refresh and replay;
- every replay reruns affected checks;
- branch head is checkpointed after each accepted push;
- a failed push does not mark the task complete.

### 5.13.6 Push cadence

Supported:

```text
after-atomic-group
after-accepted-task
after-wave
manual-command
```

10x default (canonical, single source of truth — reconciled with
`multi-repository.md` §N10.2):

```yaml
push_cadence: after-atomic-group
```

`after-accepted-task` is permitted **only** when the intermediate-branch
invariant holds:

```yaml
push_cadence: after-accepted-task
intermediate_branch_invariant: buildable-and-testable
```

Without that exact invariant, `after-accepted-task` is refused rather than
silently accepted (enforced by `kernel.ValidatePushCadence`). This preserves
the “push and push” behavior — every intermediate branch state is buildable and
testable — while still applying deterministic admission and verification before
every push.

For a very small sequence touching the same files, the planner may combine tasks into one atomic group commit rather than pushing partially inconsistent intermediate states.

### 5.13.7 Direct-push safety gates

Before each push:

```text
task acceptance
focused tests
lint/format
changed-code security checks
secret scan
contract checks
configuration/toggle guard
remote branch drift check
```

After each wave:

```text
affected integration tests
independent AI review
branch health snapshot
```

Before terminal readiness:

```text
full repository checks
cross-repository contract verification
local cross-service critical flow
final AI review of every 10x branch against its admission base
open blockers and configuration-drift report
```

No PR does not mean no review.

### 5.13.8 Configuration and feature-toggle guard

The real 10x lessons include temporary configuration changes that must be restored before later merge/deployment.

Because this profile does not merge or deploy, the guard produces two categories:

```text
must_restore_before_10x-ready
may_remain_on-10x-branch-with-explicit-handoff
```

Example:

```yaml
configuration_guard:
  temporary_changes:
    - path: config/application.yaml
      key: single_tenant.enabled
      disposition: must_restore_before_10x-ready

  explicit_handoff_allowed:
    - path: config/10x-test.yaml
      reason: dedicated initiative test configuration
```

Unclassified temporary toggles block `TEN_X_BRANCH_HANDOFF_READY`.

### 5.13.9 Branch-based change set

This workflow still needs a multi-repository orchestration unit, but it contains branch heads rather than PRs:

```yaml
branch_change_set:
  id: TENX-CHANGE-2026-Q3-01
  plan: PLAN-2026-Q3-01
  status: integrating

  repositories:
    acme-tools-dashboard:
      branch: 10x-branch
      base_sha: abc111
      current_sha: abc999
      accepted_tasks: [G5, G10]
      push_count: 6

    acme-internal-tools-service:
      branch: 10x-branch
      base_sha: def111
      current_sha: def999
      accepted_tasks: [G1, G2, G3, G4, G6, G8, G9]
      push_count: 14

    acme-mail-service:
      branch: 10x-branch
      base_sha: ghi111
      current_sha: ghi999
      accepted_tasks: [G4]
      push_count: 3

    acme-company-settings:
      branch: 10x-branch
      base_sha: jkl111
      current_sha: jkl999
      accepted_tasks: [G7]
      push_count: 2

  pull_requests_created: 0
  merges_performed: 0
  deployments_performed: 0
```

### 5.13.10 Review without a pull request

Review inputs:

```text
admission base SHA
current 10x branch SHA
PLAN task mapping
acceptance evidence
repository test results
cross-repository contract results
```

Review output:

```text
branch-review-report.md
quality-gate-report.json
configuration-drift-report.json
remaining-blockers.md
```

The reviewer may request corrective commits to the same 10x branch through the Branch Integrator.

### 5.13.11 Integration without deployment

Because deployment is disabled, integration may use only:

- local multi-repository processes;
- local containers;
- approved port forwarding to an already existing environment;
- mocks and frozen contracts;
- an external environment that another authorized process already prepared.

Delivery Foundry must not:

- create a preview deployment;
- deploy to staging;
- deploy to production;
- alter an environment to make the test pass.

If the required integration can only be demonstrated by a new deployment, the workflow ends as `status: FAILED, result_code: PROVEN_BLOCKED`, or waits as `status: WAITING, reason: external-deployment`, depending on profile policy.

### 5.13.12 Stop boundary and handoff

The workflow stops after branch readiness.

Terminal result:

```text
status: SUCCEEDED
result_code: TEN_X_BRANCH_HANDOFF_READY
```

It does not emit:

```text
PR_READY
MERGED
DEPLOYED
SHIPPED
```

Handoff:

```text
TEN-X-HANDOFF/
├── PLAN.md
├── repository-branches.yaml
├── branch-change-set.yaml
├── acceptance-summary.md
├── test-and-quality-report.md
├── branch-review-report.md
├── configuration-drift-report.md
├── integration-report.md
├── blockers.md
└── NEXT-WORKFLOW.md
```

A later PR/release process is a separate extractable workflow:

```bash
make workflow-start \
  WORKFLOW=ten-x-release-review \
  INPUT=TEN-X-HANDOFF/
```

It is not started automatically by the direct-push workflow.

### 5.13.13 Notifications

The workflow emits:

```text
PLAN admitted
10x branch confirmed per repository
local worktree created
task started/checkpointed/accepted
push queued
push integrating
push succeeded/failed
remote drift detected
conflict remediation
wave completed
branch review started/completed
configuration drift detected/resolved
branch readiness passed
TEN_X_BRANCH_HANDOFF_READY
```

Telegram batching remains active. Push and failure events are P2; branch conflicts and readiness blockers are P1; security events remain P0.

### 5.13.14 Commands

```text
/run-plan <file-id> --profile organization-10x-direct-push
/tenx-status <workflow-id>
/tenx-pause <workflow-id>
/tenx-resume <workflow-id>
/tenx-push <workflow-id> <repository>
/tenx-retry-push <workflow-id> <repository>
/tenx-branch-heads <workflow-id>
/tenx-handoff <workflow-id>
/cancel <workflow-id>
```

With `push_cadence: after-accepted-task`, `/tenx-push` is normally unnecessary. It is available when a repository is configured for command-mode pushing.




---

<!-- Relocated from V11: §13.9 Real scenario: 10x across four shared branches (lines 8972-9322) -->

## 13.9 Real scenario: 10x development across four shared branches

See **D-24 — 10x Implementation Branch Mode** for the complete branch-handoff flow.

### Scenario source

The attached 10x process describes a coordinated AI-assisted delivery cycle in which:

- `PLAN.md` is the shared execution contract;
- QA test planning and reviewed ATDD precede implementation;
- independent groups run in parallel;
- four repositories use `10x-branch`;
- bounded agents implement backend, frontend, review, and verification work;
- quality and cross-service checks continue throughout the cycle.

The Q3 execution plan further specifies that the team creates the 10x initiative branch and repeatedly pushes work to it during implementation, while formal PR review and staging deployment happen only in later phases.

For this Foundry scenario, the requested stop boundary intentionally removes those later phases.

### Requested Foundry boundary

```text
Input:
approved PLAN.md

Repositories:
acme-tools-dashboard
acme-internal-tools-service
acme-mail-service
acme-company-settings

Remote working branch:
10x-branch in every repository

Allowed:
clone/fetch
local worktrees
local task commits
direct serialized push to 10x-branch
tests, lint, Sonar/quality checks where callable
AI review
local/callable integration checks
Telegram notifications

Forbidden:
remote task branches
pull requests
merge to master/main
preview deployment
staging deployment
production deployment
```

### Configuration

```yaml
profile:
  name: acme-10x-direct-push
  extends: organization-10x-direct-push

workflow:
  entrypoint: existing-plan
  name: ten-x-direct-push

repositories:
  acme-tools-dashboard:
    target_branch: 10x-branch

  acme-internal-tools-service:
    target_branch: 10x-branch

  acme-mail-service:
    target_branch: 10x-branch

  acme-company-settings:
    target_branch: 10x-branch

branch_delivery:
  mode: direct-push
  push_cadence: after-accepted-task
  create_remote_task_branches: false
  serialize_per_repository: true
  force_push: forbidden

pull_requests:
  enabled: false

merge:
  enabled: false

deployment:
  preview:
    mode: disabled
  staging:
    mode: disabled
  production:
    mode: disabled

completion:
  result_code: TEN_X_BRANCH_HANDOFF_READY
```

### Step 1 — Upload the approved plan

```text
Upload PLAN.md
/run-plan <file-id> --profile acme-10x-direct-push
```

Foundry skips:

```text
ideation
market validation
spec generation
PLAN generation
```

It still verifies that the plan references final QA/ATDD evidence and that task/test IDs are traceable.

Notification:

```text
📥 10x PLAN received

Mode: Direct PLAN
Profile: acme-10x-direct-push
Ideation: skipped
Plan generation: skipped
Admission: starting
```

### Step 2 — Admit repositories and branches

Expected mapping:

| Repository | Required branch | Responsibility |
|---|---|---|
| `acme-tools-dashboard` | `10x-branch` | Multi-/single-tenant UI, company and subscription flows, license upload/download |
| `acme-internal-tools-service` | `10x-branch` | Company, subscription, license, user-management, provisioning, anti-tamper |
| `acme-mail-service` | `10x-branch` | Onboarding/auth and invitation email endpoint |
| `acme-company-settings` | `10x-branch` | Single-tenant invitation flow and Mail Service integration |

Admission fails if one repository points to an old branch or wrong local folder.

### Step 3 — Prepare local worktrees

Example:

```text
workspaces/flows/flow-10x-001/
├── repositories/
│   ├── acme-tools-dashboard/
│   ├── acme-internal-tools-service/
│   ├── acme-mail-service/
│   └── acme-company-settings/
└── worktrees/
    ├── acme-tools-dashboard/G5-G10/
    ├── acme-internal-tools-service/G1-G4/
    ├── acme-internal-tools-service/G6-G9/
    ├── acme-mail-service/G4/
    └── acme-company-settings/G7/
```

Every worktree starts from the latest confirmed `origin/10x-branch`.

No remote task branch is created.

### Step 4 — Execute PLAN groups

Illustrative routing:

```text
G1–G4, G6, ungrouped backend coordination
→ backend/implementation agents

G5 and G10 frontend flows
→ frontend implementation agent

G7 acme-company-settings
→ backend implementation agent

G8/G9 environment, anti-tamper, and identity work
→ backend + verification agents

QA automation and E2E evidence
→ verification/testing agent
```

PEC schedules independent work in parallel and preserves dependency order inside each group.

### Step 5 — Push accepted work immediately

Example for `acme-internal-tools-service`:

```text
TASK-G2 accepted locally
→ commit 81a2f7 created
→ Branch Integrator locks acme-internal-tools-service
→ fetch origin/10x-branch
→ replay commit on latest head
→ run focused Ginkgo tests and lint
→ push normally to origin/10x-branch
→ record new remote SHA
```

Telegram:

```text
⬆️ 10x push completed

Repository: acme-internal-tools-service
Branch: 10x-branch
Task: G2
Commit: 81a2f7
Checks: passed
Remote head: def999
PR created: no
Deployment: no
```

### Step 6 — Handle simultaneous work safely

Backend and frontend agents may work simultaneously, but each repository has one push queue.

```text
parallel local work
→ serialized integration into each repository's 10x-branch
```

If another engineer pushes first:

```text
remote SHA changed
→ fetch
→ replay local accepted commit
→ detect conflict
→ focused remediation
→ rerun checks
→ normal push
```

No force push is allowed.

### Step 7 — Apply lessons from the real session

The workflow enforces:

- verify the correct repository and branch before execution;
- require reviewed test-plan/ATDD references;
- synchronize test IDs;
- prefer enhancing existing behavior over duplicated mirrored logic;
- detect missing schema/tables/triggers as blockers;
- require explicit cluster-identity contracts;
- classify ambiguous start/end-date semantics as a product command;
- check temporary configuration toggles before readiness;
- record dependency/image failures without pretending the flow passed.

### Step 8 — Cross-repository validation without deployment

The Foundry runs what the profile permits:

```text
repository-local tests
contract checks
local FE/BE integration
local service composition
approved port-forward checks against an existing environment
critical-flow verification
```

It does not deploy a new staging environment.

If staging deployment is required to prove acceptance, status becomes:

```text
WAITING_FOR_EXTERNAL_DEPLOYMENT
```

or:

```text
PROVEN_BLOCKED
```

depending on whether another authorized process is expected to provide it.

### Step 9 — Final branch review

For each repository:

```text
admission base of 10x-branch
→ current 10x-branch
→ review all PLAN-mapped changes
→ check tests and acceptance IDs
→ inspect configuration drift
→ create branch review report
```

Corrective commits are pushed through the same serialized Branch Integrator.

No PR is created.

### Step 10 — Stop at branch readiness

Final notification:

```text
✅ 10x branches ready

Plan: PLAN-2026-Q3-01
Repositories: 4
Tasks: 23/23 accepted

Branches:
- acme-tools-dashboard/10x-branch
- acme-internal-tools-service/10x-branch
- acme-mail-service/10x-branch
- acme-company-settings/10x-branch

Pull requests created: 0
Merges performed: 0
Deployments performed: 0

Repository checks: passed
Cross-repository contracts: passed
Configured integration checks: passed
Configuration drift: clean

Status: SUCCEEDED (result_code: TEN_X_BRANCH_HANDOFF_READY)
```

### What happens next

Nothing happens automatically.

A later human or separate Foundry workflow may:

```text
review 10x branch against master
open PR
merge
deploy to staging
run full QA
release
```

Those actions are outside this scenario and require an explicitly started workflow.


