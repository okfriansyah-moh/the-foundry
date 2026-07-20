# CLI, Makefile Surface, and CI Portability

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: Interface reference (added file beyond the prescribed tree; recorded in the review report).


---

<!-- Relocated from V11: §9–9.3 Root Makefile interface (lines 7428-7825) -->

## 9. Root Makefile interface

The root Makefile is a developer convenience wrapper; the canonical runtime interfaces are the API and `foundry` CLI.

```text
Configuration
  make configure
  make profile-list
  make profile-use
  make profile-show
  make profile-validate

Installation
  make bootstrap
  make auth
  make up
  make down
  make restart

Validation
  make doctor
  make smoke
  make adapter-doctor
  make security-check

Operation
  make status
  make logs
  make plan-ingest
  make plan-admit
  make plan-execute
  make plan-status
  make repository-resolve
  make workspace-prepare
  make change-set-status
  make branch-change-set-status
  make tenx-start
  make tenx-status
  make tenx-branch-heads
  make tenx-handoff
  make integration-status
  make workflow-list
  make workflow-show
  make workflow-validate
  make workflow-start
  make workflow-status
  make workflow-pause
  make workflow-resume
  make workflow-cancel
  make workflow-migrate
  make step-list
  make step-show
  make step-run
  make step-export
  make step-submit-result

Repository
  make repo-list
  make repo-create
  make repo-clone
  make branch-create
  make change-request-create

Tracker
  make work-search
  make work-get
  make work-create
  make work-transition
  make work-comment

Documentation
  make doc-search
  make doc-get
  make doc-create
  make doc-update

Agents
  make agent-status
  make agent-run
  make agent-review
  make agent-fallback

CI and delivery
  make ci-trigger
  make ci-status
  make preview
  make deploy
  make deploy-command
  make release
  make rollback

Plugins
  make plugin-search
  make plugin-inspect
  make plugin-fetch
  make plugin-scan
  make plugin-test
  make plugin-shadow
  make plugin-canary
  make plugin-activate
  make plugin-bind
  make plugin-disable
  make plugin-revoke
  make plugin-list
  make plugin-doctor

Notifications
  make notification-status
  make notification-test
  make notification-retry
  make notification-dead-letter
  make telegram-status
  make telegram-health
  make telegram-limits
  make telegram-queue
  make telegram-batches
  make telegram-batch-flush
  make telegram-calibrate
  make telegram-simulate-429
  make telegram-load-test

Maintenance
  make backup
  make restore
  make update-check
  make update-stage
  make update-apply
```

---


## 9.1 Makefile contract for agents and skills

The root Makefile must expose a stable interface even when the underlying providers change.

```text
Agent and skill lifecycle
  make agents-list
  make agents-validate
  make agents-install
  make agents-doctor
  make skills-list
  make skills-validate
  make skills-sync
  make skills-install
  make skills-doctor
  make catalog-build
  make catalog-validate

Execution
  make plan-create
  make plan-review
  make pec-run
  make task-run
  make review-run
  make verify-run

Artifacts
  make artifact-check
  make handoff-write
  make orchestration-status
```

Example invocations:

```bash
make plan-create \
  PROFILE=personal-github \
  REPOSITORY=workspaces/products/api-changelog-assistant \
  SOURCE=.foundry/context/SPEC.md

make pec-run \
  PROFILE=personal-github \
  REPOSITORY=workspaces/products/api-changelog-assistant \
  PLAN=docs/PLAN.md

make review-run \
  PROFILE=personal-github \
  REPOSITORY=workspaces/products/api-changelog-assistant \
  CHANGE_REQUEST=42

make verify-run \
  PROFILE=personal-github \
  REPOSITORY=workspaces/products/api-changelog-assistant \
  SCOPE=changed
```

Expected `make agents-doctor` checks:

```text
[PASS] planning agent references an installed planning skill
[PASS] pec requires an approved PLAN.md
[PASS] implementation cannot run without a task block
[PASS] reviewer is read-only for production code
[PASS] verification has testing references
[PASS] no agent/skill name collision
[PASS] product-local overrides pass schema validation
```

Expected `make skills-doctor` checks:

```text
[PASS] all SKILL.md front matter parses
[PASS] every reference path exists
[PASS] guardrails enabled for production-writing agents
[PASS] quality and security thresholds are explicit
[PASS] organization profile uses only approved packages
```



## 9.2 LLM capability and methodology Makefile surface

```text
Capability discovery
  make llm-capabilities-sync
  make llm-capabilities-list
  make llm-capabilities-show PROVIDER=<provider> MODEL=<model>
  make llm-capabilities-diff
  make llm-capabilities-approve ID=<change>

Planning and execution
  make llm-envelope TASK=<task>
  make llm-token-count TASK=<task>
  make llm-cost-estimate TASK=<task>
  make llm-run TASK=<task>
  make llm-batch-submit INPUT=<manifest>
  make llm-batch-status ID=<batch>

Context optimization
  make llm-cache-report WORKFLOW=<id>
  make llm-cache-diagnose WORKFLOW=<id>
  make llm-compaction-test AGENT=<agent>
  make llm-context-report WORKFLOW=<id>
  make llm-context-replay WORKFLOW=<id>

Evaluation
  make llm-benchmark PROFILE=<profile>
  make llm-shadow CANDIDATE=<config>
  make llm-canary CANDIDATE=<config>
  make llm-promote CANDIDATE=<config>
  make llm-rollback VERSION=<version>

Methodology packs
  make methodology-list
  make methodology-fetch PACK=superpowers
  make methodology-scan PACK=superpowers
  make methodology-install PACK=superpowers PROFILE=<profile>
  make methodology-conflicts PACK=superpowers
  make methodology-test PACK=superpowers
  make methodology-pin PACK=superpowers REF=<commit-sha>
  make methodology-remove PACK=superpowers
```

`make llm-capabilities-sync` stages changes only. It must not enable a newly discovered feature.

Expected output:

```text
Anthropic capability sync

[UNCHANGED] adaptive thinking — GA
[UNCHANGED] effort — GA
[NEW] task budgets — Beta
[NEW] fast mode — Research Preview
[CHANGED] compaction supported model set
[NEW] advisor tool — Beta

No runtime configuration changed.
Review with:
make llm-capabilities-diff
```



## 9.3 Plugin, workflow, step, deployment, and notification Makefile surface

```text
Plugin lifecycle
  make plugin-search QUERY="coding agent"
  make plugin-inspect SOURCE=https://github.com/example/project
  make plugin-fetch NAME=<plugin>
  make plugin-scan NAME=<plugin>
  make plugin-test NAME=<plugin>
  make plugin-shadow NAME=<plugin> AGAINST=<active-plugin>
  make plugin-canary NAME=<plugin> PERCENT=5
  make plugin-activate NAME=<plugin>
  make plugin-bind ROLE=executor.default PLUGIN=<plugin>
  make plugin-disable NAME=<plugin>
  make plugin-revoke NAME=<plugin>
  make plugin-list
  make plugin-doctor

Workflow composition
  make workflow-list
  make workflow-show WORKFLOW=<name>
  make workflow-validate WORKFLOW=<name>
  make workflow-render WORKFLOW=<name> PROFILE=<profile>
  make workflow-diff BASE=<version> CANDIDATE=<version>
  make workflow-migrate WORKFLOW=<id> VERSION=<version>

Direct PLAN execution
  make plan-ingest PLAN=<path>
  make plan-admit PLAN=<path> PROFILE=<profile>
  make plan-execute PLAN=<path> PROFILE=<profile>
  make plan-status WORKFLOW=<id>
  make repository-resolve WORKFLOW=<id>
  make workspace-prepare WORKFLOW=<id>
  make workspace-status WORKFLOW=<id>
  make change-set-show WORKFLOW=<id>
  make integration-run WORKFLOW=<id>

10x direct-push
  make tenx-start PLAN=<path> PROFILE=organization-10x-direct-push
  make tenx-status WORKFLOW=<id>
  make tenx-queue WORKFLOW=<id>
  make tenx-push WORKFLOW=<id> REPOSITORY=<alias>
  make tenx-retry-push WORKFLOW=<id> REPOSITORY=<alias>
  make tenx-branch-heads WORKFLOW=<id>
  make tenx-branch-review WORKFLOW=<id>
  make tenx-handoff WORKFLOW=<id>
  make tenx-stop WORKFLOW=<id>

Standalone steps
  make step-list
  make step-show STEP=<step>
  make step-run STEP=<step> INPUT=<path>
  make step-export STEP=<step> INPUT=<path>
  make step-submit-result PACKET=<path> RESULT=<path>
  make step-mode WORKFLOW=<id> STEP=<step> MODE=<mode>
  make step-replace WORKFLOW=<id> STEP=<step> PLUGIN=<plugin>
  make step-disable WORKFLOW=<id> STEP=<step>

Deployment
  make deploy WORKFLOW=<id>
  make deploy-command WORKFLOW=<id>
  make deploy-status WORKFLOW=<id>
  make deploy-cancel WORKFLOW=<id>
  make rollback WORKFLOW=<id>

Notifications
  make notification-status
  make notification-test CHANNEL=telegram
  make notification-retry EVENT=<id>
  make notification-dead-letter
  make notification-replay EVENT=<id>
  make notification-verbosity LEVEL=step-progress
  make telegram-status
  make telegram-health
  make telegram-limits
  make telegram-queue
  make telegram-batches
  make telegram-batch-flush CHAT=<chat-id>
  make telegram-calibrate
  make telegram-simulate-429 RETRY_AFTER=17
  make telegram-load-test EVENTS=1000 CHATS=50
```

Examples:

```bash
# Automatic deployment—the default
make step-mode \
  WORKFLOW=flow-123 \
  STEP=deploy \
  MODE=auto

# Wait for Telegram command
make step-mode \
  WORKFLOW=flow-123 \
  STEP=deploy \
  MODE=command

# Replace an executor without modifying the workflow
make plugin-bind \
  ROLE=executor.default \
  PLUGIN=future-autonomous-builder

# Execute an existing PLAN.md directly
make plan-execute \
  PLAN=./docs/PLAN.md \
  PROFILE=personal-github

# Execute a PLAN.md using shared 10x branches,
# without PR, merge, or deployment
make tenx-start \
  PLAN=./docs/PLAN.md \
  PROFILE=organization-10x-direct-push

# Use only the planning capability
make step-run \
  STEP=planning \
  INPUT=SPEC.md \
  OUTPUT=docs/PLAN.md
```




---

<!-- Relocated from V11: §10 Makefile configuration flow (lines 7826-7890) -->

## 10. Makefile configuration flow

`mk/10-configure.mk` exposes both interactive and deterministic targets.

Conceptual target:

```makefile
.PHONY: configure

configure:
	@set -euo pipefail; \
	profile="$${PROFILE:-}"; \
	mode="$${MODE:-}"; \
	scm="$${SCM:-}"; \
	tracker="$${TRACKER:-}"; \
	docs="$${DOCS:-}"; \
	ci="$${CI:-}"; \
	notify="$${NOTIFY:-}"; \
	deploy="$${DEPLOY:-}"; \
	if [[ -z "$$profile" ]]; then \
		read -rp "Profile name: " profile; \
	fi; \
	if [[ -z "$$mode" ]]; then \
		select mode in venture engineering; do break; done; \
	fi; \
	if [[ -z "$$scm" ]]; then \
		select scm in github gitlab-cloud gitlab-self-managed bitbucket-cloud bitbucket-datacenter azure-devops; do break; done; \
	fi; \
	if [[ -z "$$tracker" ]]; then \
		select tracker in jira-cloud jira-datacenter github-issues gitlab-issues linear none; do break; done; \
	fi; \
	if [[ -z "$$docs" ]]; then \
		select docs in confluence-cloud confluence-datacenter repository github-wiki gitlab-wiki notion none; do break; done; \
	fi; \
	if [[ -z "$$ci" ]]; then \
		select ci in github-actions gitlab-ci bitbucket-pipelines jenkins bamboo custom; do break; done; \
	fi; \
	if [[ -z "$$notify" ]]; then \
		select notify in telegram slack slack microsoft-teams email cli; do break; done; \
	fi; \
	if [[ -z "$$deploy" ]]; then \
		select deploy in none vercel cloudflare aws gcp azure kubernetes custom; do break; done; \
	fi; \
	mkdir -p profiles/generated; \
	printf '%s\n' \
	  "api_version: foundry/v1" \
	  "kind: DeliveryProfile" \
	  "metadata:" \
	  "  name: $$profile" \
	  "mode: $$mode" \
	  "providers:" \
	  "  scm: {type: $$scm}" \
	  "  tracker: {type: $$tracker}" \
	  "  knowledge: {type: $$docs}" \
	  "  ci: {type: $$ci}" \
	  "  notifications: {type: $$notify}" \
	  "  deployment: {type: $$deploy}" \
	  > "profiles/generated/$$profile.yaml"; \
	echo "Created profiles/generated/$$profile.yaml"
```

The generated profile is only a starting point. `make profile-validate` must reject missing security and autonomy policies before the profile can run.

---



---

<!-- Relocated from V11: §22 CI portability (lines 12980-13009) -->

## 22. CI portability

The product template stores canonical pipeline intent:

```yaml
pipeline:
  verify:
    - make bootstrap
    - make lint
    - make typecheck
    - make test-unit
    - make test-integration
    - make security
    - make build
```

The selected CI adapter renders this into:

- `.github/workflows/ci.yml`
- `.gitlab-ci.yml`
- `bitbucket-pipelines.yml`
- `Jenkinsfile`
- `bamboo-specs`

The commands remain identical. Only orchestration syntax changes.

This prevents vendor migration from changing the engineering contract.

---



---

<!-- Relocated from V11: §24 Makefile bootstrap sequence (lines 13034-13068) -->

## 24. Makefile bootstrap sequence

First installation:

```bash
git clone <delivery-foundry-repository>
cd delivery-foundry

make bootstrap
make configure
make profile-use PROFILE=<profile>
make auth
make up
make doctor
make smoke
```

Personal:

```bash
make profile-use PROFILE=personal-github
make workflow-start WORKFLOW=venture-discovery
```

organization:

```bash
make profile-use PROFILE=team-atlassian
make workflow-start \
  WORKFLOW=engineering-delivery \
  WORK_ITEM=ENG-1234
```

---



---

<!-- Relocated from V11: §25 Detailed make doctor (lines 13069-13138) -->

## 25. Detailed `make doctor`

`make doctor` validates the active profile only.

Checks:

### System

- Git;
- Docker;
- Docker Compose;
- Make;
- curl;
- jq;
- yq;
- filesystem permissions;
- required ports.

### Profile

- schema validity;
- all provider types exist;
- no personal/work policy conflict;
- allowlists and denylists;
- autonomy policy;
- data classification;
- notification policy.

### Providers

- SCM authentication;
- tracker authentication;
- knowledge authentication;
- CI access;
- deployment access;
- notification access.

### Agents

- Claude Code;
- Codex;
- Cursor;
- Copilot;
- OpenCode;
- OpenHands.

Only agents permitted by the profile must pass.

### Security

- secrets not committed;
- personal tokens absent from organization profile;
- work tokens absent from personal profile;
- workspace root allowed;
- external proxy disabled where required;
- notification destination approved;
- repository access scoped.

### Smoke test

- read one harmless repository resource;
- read one harmless work item when configured;
- read one harmless documentation page when configured;
- send a redacted test notification;
- create and delete a disposable local workspace;
- do not mutate production systems.

---




---

<!-- Relocated from V11: §25.1 Security/capability/memory/recovery Makefile surface (lines 13139-13211) -->

## 25.1 Security, capability, memory, and recovery Makefile surface

```text
Capability discovery and evolution
  make capability-search QUERY="..."
  make capability-list
  make capability-show NAME=<name>
  make capability-fetch NAME=<name>
  make capability-scan NAME=<name>
  make capability-test NAME=<name>
  make capability-stage NAME=<name>
  make capability-promote NAME=<name>
  make capability-revoke NAME=<name>
  make capability-rollback NAME=<name>
  make capability-audit

Prompt injection
  make injection-test
  make context-scan INPUT=<path>
  make trust-report WORKFLOW=<id>

Supply chain
  make deps-lock-check
  make deps-provenance-check
  make deps-signature-check
  make deps-vulnerability-scan
  make deps-malware-check
  make deps-license-check
  make sbom
  make image-scan
  make action-pin-check

Memory
  make memory-status
  make memory-candidate WORKFLOW=<id>
  make memory-review ID=<id>
  make memory-promote ID=<id>
  make memory-revoke ID=<id>
  make memory-contradictions
  make memory-retention-run
  make memory-rebuild-index

Recovery
  make recovery-status WORKFLOW=<id>
  make recovery-simulate WORKFLOW=<id>
  make recovery-run WORKFLOW=<id>
  make circuit-breaker-status
  make rollback WORKFLOW=<id>
  make pause-all
```

`make security-check` must include:

```text
[PASS] active profile isolation
[PASS] immutable policy kernel checksum
[PASS] no Docker socket mounted
[PASS] no host home directory mounted
[PASS] network egress default deny
[PASS] secret broker reachable
[PASS] capability registry signatures/checksums valid
[PASS] no quarantined package is active
[PASS] third-party CI actions pinned immutably
[PASS] dependency lockfiles valid
[PASS] install-script policy strict
[PASS] vulnerability and malware scans clean or explicitly accepted
[PASS] prompt-injection regression suite
[PASS] memory namespace and poisoning controls
[PASS] rollback assets available
```





---

<!-- Relocated from V11: §25.2 Capacity/checkpoint/liveness Makefile surface (lines 13212-13287) -->

## 25.2 Capacity, checkpoint, restart, and liveness Makefile surface

```text
Provider capacity
  make capacity-sync
  make capacity-status
  make capacity-status PROVIDER=anthropic
  make capacity-forecast TASK=<task>
  make capacity-reserve TASK=<task>
  make capacity-release RESERVATION=<id>
  make capacity-drain PROVIDER=<provider>
  make capacity-circuit-status
  make capacity-history PROVIDER=<provider>

Checkpoint and resume
  make checkpoint-create WORKFLOW=<id>
  make checkpoint-show WORKFLOW=<id>
  make checkpoint-verify WORKFLOW=<id>
  make checkpoint-restore WORKFLOW=<id>
  make workflow-resume WORKFLOW=<id>
  make workflow-restart WORKFLOW=<id>
  make workflow-rollover WORKFLOW=<id>
  make workflow-failover WORKFLOW=<id> PROVIDER=<provider>
  make workflow-wake WORKFLOW=<id>

Liveness
  make liveness-status
  make liveness-status WORKFLOW=<id>
  make liveness-doctor
  make liveness-watchdog-run
  make orphan-list
  make orphan-recover WORKFLOW=<id>
  make no-progress-report WORKFLOW=<id>

Retry simulation
  make retry-policy-check
  make retry-simulate ERROR=429
  make retry-simulate ERROR=subscription-limit
  make retry-simulate ERROR=context-full
  make retry-simulate ERROR=provider-outage
  make reset-window-test PROVIDER=<provider>
```

Example status:

```text
Workflow flow-456

State: WAITING_FOR_SUBSCRIPTION_RESET
Task: TASK-014
Provider: claude-code
Checkpoint: checkpoint-789 [verified]
Last progress: 2026-07-18 11:42 UTC
Reset observed: 2026-07-18 16:00 UTC
Wake scheduled: 2026-07-18 16:00:15 UTC
Fallback: codex [allowed but lower priority]
Total attempts: 7
Consecutive same error: 0
Compute currently allocated: no

Non-stall invariant: PASS
```

`make liveness-doctor` must fail when:

- a running workflow has no heartbeat;
- a waiting workflow has no `wake_at` or event subscription;
- a checkpoint is missing or corrupt;
- two workers hold active write leases;
- a retry policy permits an unbounded immediate loop;
- a provider is selected without sufficient reservation;
- a subscription reset is known but not scheduled;
- a workflow is repeatedly retrying without progress;
- a capacity state is being treated as unlimited because it is unknown.


