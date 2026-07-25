# Capability Evolution Loop

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative workflow.** Promotion authority is bounded by `../autonomy/cumulative-drift-governance.md` (L0–L4); learned capabilities auto-activate only inside the pre-authorized personal envelope.


---

<!-- Relocated from V11: §5.5 Capability Evolution Loop (lines 3660-3941) -->

## 5.5 Capability Evolution Loop

Delivery Foundry may add agents and skills over time, but it must treat every capability as executable supply-chain material.

The capability loop is:

```text
observe repeated work or failure
        ↓
identify a capability gap
        ↓
search installed catalog
        ↓
search approved registries or public sources
        ↓
reuse, adapt, or generate a candidate
        ↓
quarantine
        ↓
scan and evaluate
        ↓
stage in one sandboxed workflow
        ↓
measure
        ↓
approve, promote, reject, or revoke
```

This is the difference between a fixed agent collection and a genuine self-improving delivery system.

### 5.5.1 Agent versus skill decision

Use this rule:

```text
new procedure, checklist, knowledge, or verification method
→ skill

new persistent responsibility, state owner, delegation role, or escalation boundary
→ agent
```

Examples:

| Capability gap | Preferred abstraction |
|---|---|
| PostgreSQL migration review | Skill |
| API/MCP contract verification | Skill |
| Kubernetes environment readiness | Skill |
| Accessibility browser audit | Skill |
| Cross-repository integration coordinator | Agent |
| Capability discovery and promotion manager | Agent |
| Recovery and rollback coordinator | Agent |
| Durable-memory curation and contradiction handling | Agent |

Creating a new agent for every task is a defect. Most improvements should extend a reusable skill.

### 5.5.2 Capability Curator

Add:

```text
agents/capability-curator.md
```

Responsibilities:

```text
read workflow failures and repeated manual corrections
→ cluster recurring gaps
→ search the local capability catalog
→ search approved external sources
→ compare candidates
→ recommend reuse, adaptation, or generation
→ create a quarantined candidate
→ request evaluation
```

The curator cannot:

- activate a capability globally;
- grant new tool permissions;
- weaken security rules;
- change the allowed external sources;
- promote its own generated package;
- modify the immutable policy kernel;
- read unrelated secrets;
- bypass package quarantine.

### 5.5.3 Capability lifecycle

```text
PROPOSED
    ↓
DISCOVERED
    ↓
QUARANTINED
    ↓
SCANNING
    ↓
EVALUATING
    ↓
STAGED
    ↓
APPROVAL_REQUIRED
    ↓
ACTIVE
    ↓
DEPRECATED
    ↓
REVOKED
```

A candidate has no production permissions before `ACTIVE`.

### 5.5.4 Capability manifest

Every external or generated capability requires:

```yaml
api_version: foundry.capability/v1
kind: Skill

metadata:
  name: mcp-api-contract-verifier
  version: 0.1.0
  origin: generated
  owner: capability-curator
  created_at: 2026-07-18T12:00:00Z

source:
  repository: null
  commit: null
  checksum: sha256:...
  license: Apache-2.0
  provenance: generated-inside-foundry

permissions:
  filesystem:
    read:
      - docs/openapi.yaml
      - apps/mcp/**
    write:
      - docs/verification-report.md
  network:
    allowed: false
  secrets:
    allowed: []
  tools:
    - read-file
    - schema-validator

inputs:
  - OpenAPI document
  - MCP tool definitions

outputs:
  - contract compatibility report

risk:
  level: medium
  reasons:
    - reads public interface definitions

evaluation:
  fixtures:
    - compatible-contract
    - incompatible-parameter
    - missing-auth
    - oversized-output
  minimum_pass_rate: 0.95

promotion:
  minimum_runs: 10
  maximum_policy_violations: 0
  human_approval: required
```

### 5.5.5 External discovery

The system may search:

- approved GitHub organizations;
- internal repositories;
- OpenHands plugins;
- Agent Skills-compatible repositories;
- approved MCP registries;
- organization-maintained registries;
- signed package indexes.

It must not automatically trust:

- search-engine ranking;
- popularity;
- star count;
- an LLM recommendation;
- a README claim;
- a package name that resembles a trusted package;
- a branch named `main`;
- an unsigned binary.

### 5.5.6 Internet package workflow

```text
external source
→ fetch metadata only
→ evaluate origin and license
→ pin exact commit or immutable version
→ download without execution
→ calculate checksum
→ inspect content
→ scan dependencies
→ run prompt-injection scan
→ run malware and behavior evaluation
→ install in a network-restricted sandbox
→ run fixtures
→ stage in one workflow
→ promote only after evidence
```

A package discovered on the internet must never be copied directly into `skills/approved` or `agents/approved`.

### 5.5.7 Generated capabilities

When no suitable package exists, the system may generate a capability through the normal delivery discipline:

```text
capability SPEC
→ capability PLAN
→ implementation
→ correctness review
→ security review
→ fixture verification
→ sandbox canary
→ promotion gate
```

Generated code, prompts, agents, and skills are not trusted merely because Delivery Foundry created them.

### 5.5.8 Capability fitness

Measure:

```text
fixture pass rate
real task completion rate
false-positive rate
false-negative rate
human correction rate
defects introduced
defects missed
token and cost delta
execution time
repeatability
security-policy violations
```

Promotion example:

```yaml
promotion:
  minimum_fixture_pass_rate: 0.95
  minimum_real_runs: 10
  maximum_human_correction_rate: 0.15
  maximum_security_violations: 0
  maximum_cost_regression_percent: 20
```

### 5.5.9 Immutable promotion rules

The implementer, author, or curator of a capability may not be its sole reviewer.

```text
candidate author
    ≠ security reviewer
    ≠ promotion authority
```

For personal low-risk profiles, policy may allow automatic promotion after deterministic tests and a clean canary. For organization, confidential, or high-risk profiles, human approval remains mandatory.

---



---

<!-- Relocated from V11: §5.6 Secure self-improvement kernel (lines 3942-3991) -->

## 5.6 Secure self-improvement kernel

Delivery Foundry may be self-healing, self-learning, and self-adapting, but it must not be self-authorizing.

The system is split into two planes:

```text
Control plane
- immutable policy kernel
- permissions
- budgets
- approval rules
- profile isolation
- secret boundaries
- promotion rules
- kill switch

Learning plane
- agents
- skills
- routing preferences
- memory
- recovery strategies
- prompts
- capability candidates
```

The learning plane may propose changes. It cannot mutate the control plane.

The following are immutable during unattended execution:

- root trust keys;
- allowed registries;
- profile data classification;
- repository allowlists and denylists;
- tool permission ceilings;
- network-egress policy;
- secret-access policy;
- maximum spend;
- human-required gates;
- production-deployment authority;
- capability-promotion authority;
- audit retention;
- emergency kill switch;
- security and memory poisoning rules.

Changes to these controls require a separately authenticated operator or organization administrator.





---

<!-- Relocated from V11: §5.9 Superpowers methodology pack (lines 4833-5017) -->

## 5.9 Superpowers methodology pack

`obra/superpowers` is useful, but it should be integrated as a **methodology pack**, not as a second orchestration platform competing with PEC.

It provides a cross-harness skills methodology covering:

- brainstorming before implementation;
- isolated Git worktrees;
- detailed implementation plans;
- subagent-driven development;
- strict test-driven development;
- systematic debugging;
- verification before completion;
- requesting and receiving code review;
- finishing a development branch;
- writing and testing new skills.

It supports Claude Code, Codex, Cursor, GitHub Copilot CLI, OpenCode, and several other harnesses.

### 5.9.1 Integration decision

Use this hierarchy:

```text
Delivery Foundry security and profile policy
        ↓
Delivery Foundry workflow and PEC
        ↓
selected Superpowers process skills
        ↓
domain implementation skills
        ↓
provider defaults
```

Superpowers never outranks the Foundry security kernel, active profile, approved `PLAN.md`, or human-required gates.

### 5.9.2 Adopt, map, or disable

| Superpowers capability | Foundry integration |
|---|---|
| `brainstorming` | Adopt before venture/product specification and ambiguous engineering specs |
| `using-git-worktrees` | Adopt as the standard isolated-workspace procedure |
| `writing-plans` | Map to the Foundry planning agent and canonical `docs/PLAN.md` |
| `subagent-driven-development` | Disable when PEC is active; PEC is the single orchestrator |
| `executing-plans` | Map to PEC for wave execution |
| `dispatching-parallel-agents` | Reuse its heuristics under PEC's no-shared-file and dependency rules |
| `test-driven-development` | Adopt for implementation tasks where test-first is practical |
| `systematic-debugging` | Adopt for deterministic-code and environment failures |
| `verification-before-completion` | Merge into the verification agent's evidence gate |
| `requesting-code-review` | Adopt before independent review dispatch |
| `receiving-code-review` | Adopt for remediation discipline |
| `finishing-a-development-branch` | Adapt to SCM provider and Foundry release policy |
| `writing-skills` | Adopt for Capability Evolution Loop skill generation and behavioral evaluation |
| `using-superpowers` | Replace its global authority rule with Foundry's policy-aware skill resolver |

The critical constraint is:

> Never run PEC and Superpowers' autonomous plan executor against the same plan simultaneously.

That would create two schedulers, duplicate work, and conflicting state.

### 5.9.3 Skill-writing methodology

Superpowers' `writing-skills` method is highly relevant to self-adaptation:

```text
baseline pressure scenario without skill
→ observe failure
→ write minimal skill
→ replay same scenario
→ observe compliance
→ add adversarial scenarios
→ refactor and close loopholes
```

Delivery Foundry should add these evaluations to every generated skill.

A skill is not promoted merely because its Markdown looks reasonable.

### 5.9.4 Installation model

Canonical Foundry integration:

```text
pinned Superpowers repository commit
→ quarantine and scan
→ verify MIT license
→ run plugin and skill evaluations
→ map skill names
→ resolve conflicts
→ materialize through provider adapter
```

Provider installation may use official marketplaces, but Foundry records and validates the resolved package version.

Example:

```yaml
methodology_pack:
  name: superpowers
  source: https://github.com/obra/superpowers
  commit: 48e3f1985dbcd5c1ea6a8237bff28ba63931f767
  license: MIT

  enabled_skills:
    - brainstorming
    - using-git-worktrees
    - test-driven-development
    - systematic-debugging
    - verification-before-completion
    - requesting-code-review
    - receiving-code-review
    - finishing-a-development-branch
    - writing-skills

  mapped_skills:
    writing-plans: foundry-planning
    executing-plans: foundry-pec
    dispatching-parallel-agents: foundry-pec

  disabled_when_forge_active:
    - subagent-driven-development
```

The commit above is an example snapshot and must be refreshed through the normal update process rather than copied forever.

### 5.9.5 Provider installation

```bash
make methodology-install \
  PACK=superpowers \
  PROFILE=personal-github

make methodology-validate \
  PACK=superpowers

make methodology-conflicts \
  PACK=superpowers

make methodology-test \
  PACK=superpowers

make methodology-pin \
  PACK=superpowers \
  REF=<commit-sha>
```

The adapter may install the pack into Claude Code, Codex, Cursor, Copilot, or OpenCode using the harness's native plugin mechanism.

### 5.9.6 Security constraints

Superpowers is still executable agent instruction material. Apply the same controls as any external capability:

- pin the exact source;
- verify license and provenance;
- scan hooks, scripts, and install instructions;
- deny unapproved network or secret access;
- run prompt-injection evaluations;
- test provider-specific installation;
- detect conflicting skill triggers;
- preserve Foundry instruction precedence;
- prevent automatic self-update in organization profiles;
- roll back independently.

### 5.9.7 Why not replace Agent Skills or PEC?

Superpowers and the existing Foundry skills overlap, but they solve different levels:

```text
Superpowers
→ broad process methodology and cross-harness behavioral discipline

Foundry canonical agents and skills
→ organization-specific PLAN format, ATDD mapping,
  quality gates, security, memory, recovery, capability promotion

PEC
→ durable stateful wave orchestration
```

Use Superpowers to strengthen the process layer. Do not discard the Foundry control plane.



