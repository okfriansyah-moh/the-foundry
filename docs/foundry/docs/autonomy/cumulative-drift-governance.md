# Cumulative Drift Governance, Promotion Levels, and the Weekly Veto Digest

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Individually safe changes compound. Two hundred auto-promoted tweaks over a quarter is a system nobody recognizes. Drift itself is budgeted.

## 1. Promotion levels

```text
L0 — runtime parameter tuning (retry timing, batch sizing, compaction thresholds, routing weights within bounds)
L1 — prompt and skill changes (wording, context selection, task-sizing heuristics, low-risk skill procedures)
L2 — generated executable code used in sandbox only
L3 — executable plugin or new external integration
L4 — security, permission, budget, or authority change
```

Initial policy: only bounded L0 and carefully controlled L1 may auto-promote, and only after replay, shadow, and canary/synthetic evaluation, scoped to one profile, with the previous version retained and reversible. L2 requires stronger gates and usually human approval. **L3 and L4 always require human approval.** Learning components can never expand their own authority (this refines, and does not weaken, the kernel learning invariant: automatic activation exists only inside a pre-authorized, reversible, per-profile envelope).

## 2. CumulativeChangeBudget

```yaml
cumulative_change_budget:
  window: 30d
  max_promotions: int
  max_files_configs_prompts_changed: int
  max_routing_weight_movement: percent
  max_aggregate_behavioural_delta: threshold
  max_cost_delta: percent
  min_quality_delta: threshold          # regression bound
  max_rollback_chain_depth: int
  max_time_since_human_checkpoint: 14d
```

Auto-promotion FREEZES (until human review) when any of the following occurs: cumulative budget exceeded; unexplained quality regression; cost increase above threshold; security classification change; rollback-chain depth above threshold.

## 3. Weekly veto digest (non-blocking governance)

Sent through Telegram and/or the configured personal channel:

- lists every promoted change in the window;
- shows before/after metrics per change;
- shows cumulative drift budget consumption;
- links each change to one-click rollback;
- provides a 24-hour veto window;
- continues automatically when there is no veto.

The digest never blocks the loop; the freeze conditions above are the only automatic brakes. Vetoed changes roll back and are recorded as learning evidence.


---

<!-- Relocated from V11: §20.3 Self-learning (lines 11877-11958) -->

## 20.3 Self-learning

Self-learning is evidence-driven improvement, not automatic prompt mutation.

Inputs:

```text
workflow outcomes
verification findings
review findings
human corrections
time and cost
customer behavior
incident reports
blocked-time categories
capability fitness
```

Learning pipeline:

```text
collect
→ redact
→ classify
→ cluster repeated patterns
→ formulate candidate lesson
→ decide whether to:
     improve an existing skill
     create a new skill
     change routing
     add a test fixture
     change recovery policy within bounds
     create a product backlog item
→ evaluate offline
→ shadow
→ canary
→ promote
```

### 20.3.1 What may adapt automatically

Within profile-defined bounds:

- choose a better agent for a task type;
- tune task decomposition;
- add or improve test fixtures;
- improve non-security prompts;
- change retry order;
- adjust context retrieval;
- deprecate a consistently poor capability;
- optimize token usage;
- recommend new skills;
- update product-local procedural memory.

### 20.3.2 What may not adapt automatically

- security-policy ceilings;
- profile isolation;
- secret access;
- production authority;
- human-required approvals;
- data classification;
- budget maximum;
- allowed model providers for sensitive data;
- audit deletion;
- root trust;
- package-registry allowlists;
- its own promotion criteria.

### 20.3.3 Shadow and canary learning

```text
candidate v2
├── shadow: evaluate on historical tasks
├── replay: compare with accepted evidence
├── canary: 5% of low-risk tasks
├── observe: completion, quality, cost, violations
└── promote or rollback
```

Never replace a proven agent or skill globally based on one successful example.



---

<!-- Relocated from V11: §20.4 Self-adaptation (lines 11959-11984) -->

## 20.4 Self-adaptation

Adaptation selects among already authorized options.

Examples:

```text
Claude quota unavailable
→ route the next bounded planning task to an approved fallback

Cursor browser agent fails twice
→ recreate sandbox, then use approved browser-verification fallback

A dependency blocks install scripts
→ do not disable policy
→ request review of the exact package script

An agent repeatedly misses context cancellation
→ update backend skill candidate
→ evaluate
→ canary
→ promote
```

Adaptive behavior is constrained by policy, not improvised.

