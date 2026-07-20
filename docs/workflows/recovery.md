# Recovery, Retry, and Honest Completion

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative workflow.**


---

<!-- Relocated from V11: §20.2 Self-healing (lines 11757-11876) -->

## 20.2 Self-healing

Self-healing means recovering from known bounded failures without inventing unsafe actions.

Recovery ladder:

```text
L0 — retry idempotent operation with backoff
L1 — recreate clean sandbox and repeat
L2 — use a focused debugging agent
L3 — switch task-level executor
L4 — revert recent task or dependency change
L5 — disable or quarantine a suspected capability
L6 — rollback preview or production release
L7 — pause and escalate to human
```

### 20.2.1 Recovery Manager

Add:

```text
agents/recovery-manager.md
```

It reads:

- failure classification;
- workflow state;
- recent changes;
- verification evidence;
- recovery policy;
- blast radius.

It cannot:

- modify product requirements;
- expand tool permissions;
- retry indefinitely;
- suppress security alerts;
- mark verification passed;
- disable rollback;
- continue past a human-required gate.

### 20.2.2 Failure classification

```text
TRANSIENT
DETERMINISTIC_CODE
DEPENDENCY
ENVIRONMENT
AUTHORIZATION
RESOURCE_EXHAUSTION
PROMPT_INJECTION
SUPPLY_CHAIN
DATA_CORRUPTION
UNKNOWN
```

Example policy:

```yaml
recovery:
  transient:
    max_attempts: 3
    backoff: exponential

  deterministic_code:
    same_agent_retries: 1
    debugger_retries: 1
    fallback_executor_retries: 1

  prompt_injection:
    action: contain-and-pause

  supply_chain:
    action: quarantine-and-pause

  data_corruption:
    action: rollback-and-escalate

  unknown:
    max_attempts: 0
    action: pause
```

### 20.2.3 Circuit breakers

Circuit breakers stop loops when:

- the same error repeats;
- cost rises without accepted output;
- verification score declines;
- external service fails repeatedly;
- a model repeatedly violates policy;
- memory contradiction count spikes;
- dependency scans fail;
- unauthorized tool calls occur.

No autonomous system should convert persistence into infinite retries.

### 20.2.4 Rollback

Rollback assets:

```text
Git commit and branch
dependency lockfile
capability version
agent and skill catalog version
prompt version
memory checkpoint
database backup or migration down-plan
deployment artifact
container digest
configuration snapshot
```

The system must be able to roll back a learned capability independently of product code.



---

<!-- Relocated from V11: §20.9 Retry policy without stalling (lines 12583-12697) -->

## 20.9 Retry policy without stalling or hot-looping

“No maximum retry” is acceptable only at the workflow-lifetime level.

It is unsafe at the same-error or same-strategy level.

Correct distinction:

```text
workflow total lifetime attempts
→ may be unlimited

consecutive identical failure
→ always bounded

same provider/strategy retries
→ always bounded

request frequency
→ always rate-limited and backoff-controlled
```

### 20.9.1 Recommended policy

```yaml
liveness:
  workflow_lifetime: unlimited
  maximum_total_attempts: null

  heartbeat_interval: 30s
  watchdog_interval: 60s
  stale_after: 5m
  no_progress_after: 30m

retry:
  honor_retry_after: true
  honor_provider_reset_at: true
  reset_grace: 15s

  base_interval: 30s
  multiplier: 2
  jitter: full
  maximum_interval: 6h

  max_consecutive_same_error: 2
  max_attempts_same_strategy: 3
  max_immediate_retries: 1

  after_same_error_limit:
    - checkpoint
    - root-cause-classify
    - change-strategy
    - circuit-break-provider-if-needed

  notifications:
    - after: 10m
    - after: 1h
    - after: 24h
    - every: 24h

  waiting:
    keep_scheduled: true
    release_compute: true
    preserve_checkpoint: true
```

### 20.9.2 Error-specific retry behavior

| Failure | Action |
|---|---|
| HTTP 429 with `retry-after` | Checkpoint, wait exactly as instructed plus jitter/grace |
| Known subscription reset | Checkpoint, schedule wake just after reset |
| 5xx/provider overload | Jittered exponential backoff, then provider failover |
| Network timeout | Retry once idempotently, then recreate client/sandbox |
| Context full | Compact or fresh-session rollover; do not resend unchanged oversized request |
| Monthly allowance exhausted | Authorized credits, fallback, or wait for reset |
| API balance/spend cap | Hard budget gate; never retry paid calls blindly |
| Authentication revoked | Pause and notify; retrying credentials is pointless |
| Deterministic compile/test failure | Debug workflow; provider retry alone is not a solution |
| Prompt injection/supply chain | Contain, quarantine, and pause |
| Human-required approval | Wait and remind; do not bypass |
| Unknown | Checkpoint and pause after one classification attempt |

### 20.9.3 Retry scheduling

Every nonterminal waiting workflow must have:

```text
reason
next_action
wake_at
checkpoint_id
notification_state
owner
```

A workflow with no `wake_at`, no event subscription, and no human gate is considered stalled and is automatically repaired by the liveness watchdog.

### 20.9.4 Waiting is not failure

Valid waiting reasons (canonical form — see `../architecture/state-model.md`):

```text
status: WAITING, reason: rate-reset
status: WAITING, reason: subscription-reset
status: WAITING, reason: provider-outage
status: WAITING, reason: budget
status: WAITING, reason: human-approval
status: WAITING, reason: blocked-dependency
```

These workflows consume no model tokens while waiting.

---



---

<!-- Relocated from V11: §20.11 Honest completion guarantee (lines 12778-12838) -->

## 20.11 Honest completion guarantee

No system can unconditionally guarantee that every requested task finishes.

Completion can remain impossible when:

- the requirement is contradictory or unsatisfiable;
- every approved provider is permanently unavailable;
- credentials are revoked and never restored;
- a hard human gate is never answered;
- a required external dependency disappears;
- the budget is permanently zero;
- the repository cannot build;
- legal or security policy forbids the action;
- the acceptance criterion is undecidable.

Delivery Foundry therefore defines two explicit contracts.

### 20.11.1 Non-Stall Guarantee

Under a healthy Foundry control plane:

1. No accepted workflow silently disappears.
2. Every nonterminal workflow has a live lease, event subscription, human gate, or future `wake_at`.
3. Every meaningful change is checkpointed durably.
4. Every failed attempt produces a retry, strategy change, wait state, rollback, or escalation.
5. No identical failure hot-loops indefinitely.
6. Provider resets and restarts are handled automatically.
7. A status query can always explain the current state and next action.

### 20.11.2 Eventual Completion Guarantee under assumptions

A workflow is expected to eventually complete when all of these remain true:

```text
the objective is satisfiable
acceptance criteria are decidable
at least one approved capable executor eventually becomes available
required dependencies eventually recover
credentials eventually become valid
budget policy permits the work
required human gates eventually resolve
```

When those assumptions do not hold, the system must produce a **Proven Blocked** outcome with evidence rather than falsely claiming completion.

### 20.11.3 Terminal outcomes

```text
COMPLETED
CANCELLED
REJECTED
ROLLED_BACK
PROVEN_BLOCKED
SECURITY_TERMINATED
```

A workflow may remain in an explicit waiting state indefinitely, but it may not remain invisible or unscheduled.

---



---

<!-- Relocated from V11: §21 Retry and idempotency (lines 12951-12979) -->

## 21. Retry and idempotency

Rules:

1. Every external action has an idempotency key.
2. Replaying a webhook cannot create duplicate branches, issues, pages, pull requests, releases, or provider jobs.
3. Same-error and same-strategy retries are limited even when total workflow lifetime is unlimited.
4. Provider fallback is task-level, not turn-level.
5. Repeated identical failures trigger checkpoint, classification, strategy change, wait, or escalation.
6. Every retryable workflow has a durable checkpoint and future wake-up or event subscription.
7. Ambiguous requirements never receive invented defaults unless the policy explicitly defines one.
8. Merge, deployment, deletion, spending, credit purchase, and publication are separately gated.
9. A restarted worker must acquire a new fencing token before writing.
10. Provider request IDs and Foundry idempotency keys are retained for troubleshooting and replay protection.

Example idempotency key:

```text
<profile>:<workflow-id>:<operation>:<resource>
```

Example:

```text
team-atlassian:flow-123:change-request.create:platform-api-service
```

---

