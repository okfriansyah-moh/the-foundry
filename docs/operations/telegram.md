# Notifications and the Telegram Engine

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Telegram is a notification and low-risk command surface. It is never a valid approval channel for high-risk or organization actions (`../security/approval-and-provenance.md`). Verify current Telegram Bot API limits at implementation time.


---

<!-- Relocated from V11: §19 Complete process notification policy (lines 10648-11544) -->

## 19. Complete process notification policy

Delivery Foundry emits a notification event for every workflow process and state transition.

The default is:

```yaml
notifications:
  enabled: true
  granularity: step-progress
  delivery: durable-outbox

  lifecycle_events:
    - scheduled
    - started
    - progress
    - checkpointed
    - waiting
    - command-required
    - retry-scheduled
    - retry-started
    - provider-failover
    - session-rollover
    - completed
    - skipped
    - disabled
    - failed
    - rolled-back
    - proven-blocked

  command_channel: telegram
```

### 19.1 Notification granularity

| Level | Coverage |
|---|---|
| `step` | Every workflow node lifecycle |
| `step-progress` | Every node plus milestones, checkpoints, retries, waits, and capacity transitions |
| `operation` | Every tool invocation, command, file mutation, and model call |
| `exceptions` | Only decisions, failures, gates, and security events |

Default:

```text
step-progress
```

The user requested every process notification. `step-progress` covers every process while preventing thousands of messages for individual file reads.

`operation` is available for debugging and audit sessions.

### 19.2 Universal event envelope

```json
{
  "event_id": "notify-123",
  "workflow_id": "flow-456",
  "workflow_version": "1.0.0",
  "step_id": "build-backend",
  "step_attempt": 2,
  "event_type": "step.retry-scheduled",
  "severity": "warning",
  "profile": "personal-github",
  "plugin": "codex-native@1.2.0",
  "summary": "Backend task will retry after provider reset.",
  "progress": {
    "completed": 4,
    "total": 7
  },
  "checkpoint_id": "checkpoint-789",
  "next_action": "resume",
  "wake_at": "2026-07-18T16:00:15Z",
  "requires_command": false,
  "occurred_at": "2026-07-18T12:00:00Z",
  "idempotency_key": "flow-456:build-backend:retry-2"
}
```

### 19.3 Events emitted by every process

#### Mission and portfolio

- mission received;
- mission normalized;
- policy generated;
- budget reserved;
- discovery started/completed;
- ideas generated;
- scoring started/completed;
- candidate rejected;
- top candidates selected;
- validation started/progress/completed;
- product selected or no-build decision.

#### Direct PLAN intake

- PLAN uploaded;
- artifact classified;
- admission started/completed;
- automatic structural repair;
- command required;
- repository resolution;
- mirror fetch;
- workspace/worktree creation;
- baseline verification;
- branch creation;
- cross-repository change-set creation;
- integration environment creation.

#### Specification and planning

- context gathering;
- source loaded;
- ambiguity detected;
- command or answer required;
- specification drafted;
- specification reviewed;
- PLAN generation;
- PLAN validation;
- wave and task creation;
- plan approved or rejected.

#### Agent execution

- plugin selected;
- capacity reserved;
- sandbox created;
- task started;
- progress milestone;
- test started/completed;
- checkpoint created;
- context compacted;
- session rolled over;
- provider failed over;
- task completed;
- task remediation;
- task blocked.

#### Review and verification

- review started;
- findings generated;
- security review;
- quality gate;
- verification command;
- evidence accepted/rejected;
- remediation wave;
- verification completed.

#### Source control and CI

- 10x target branch confirmed;
- direct push queued;
- Branch Integrator lease acquired;
- remote drift detected;
- accepted commit replayed;
- direct push succeeded/failed;
- branch readiness review;
- branch created;
- commit created;
- push completed;
- PR/MR created;
- reviewer assigned;
- pipeline scheduled;
- pipeline started;
- pipeline step failed;
- pipeline retry;
- pipeline passed;
- merge scheduled/completed.

#### Deployment

- release prepared;
- preflight started/completed;
- deployment mode resolved;
- deployment command requested;
- deployment command accepted/rejected;
- deployment started;
- migration started/completed;
- health check;
- stabilization;
- deployment completed;
- rollback started/completed.

#### Growth and learning

- analytics snapshot;
- hypothesis generated;
- improvement selected;
- product pivot recommendation;
- product kill recommendation;
- memory candidate;
- capability gap;
- plugin discovered;
- plugin quarantined/scanned/shadowed/canaried/activated/revoked.

#### Capacity and recovery

- capacity constrained;
- admission paused;
- retry scheduled;
- provider reset discovered;
- workflow waiting;
- liveness repair;
- orphan detected/recovered;
- circuit breaker opened/closed;
- workflow proven blocked.

#### Security

Security events are always immediate and cannot be muted:

- prompt injection detected;
- unauthorized tool call;
- secret access rejected;
- dependency malware alert;
- policy violation;
- capability quarantined;
- credentials revoked;
- containment started;
- emergency pause;
- security recovery completed.

### 19.4 Telegram message behavior

Do not create a new noisy message for every progress tick by default.

Use one threaded or editable message per workflow step:

```text
Step: Build public API
Status: RUNNING
Progress: 4/7 acceptance criteria
Agent: Codex
Attempt: 2
Cost: $1.84
Last checkpoint: 2 minutes ago
Next: integration verification
```

The adapter updates that message as progress changes.

Immediate separate messages are reserved for:

- command required;
- failure;
- security;
- provider wait;
- deployment;
- rollback;
- product shipped;
- first revenue.

### 19.5 Durable notification outbox

Notifications must survive service restarts.

```text
workflow transaction
→ write state transition
→ write notification event to outbox
→ commit
→ notification worker sends
→ persist delivery receipt
```

On failure:

```text
retry with backoff
→ alternative configured channel
→ dead-letter queue
→ dashboard warning
```

A notification failure normally does not stop product work. A `command` step cannot proceed until its command request has at least one confirmed delivery.

### 19.6 Delivery guarantees

Notification delivery supports:

```text
at-least-once transport
+ event idempotency
+ message update/deduplication
+ delivery receipt
```

The Telegram command handler validates:

- user identity;
- chat identity;
- workflow and step;
- current state;
- nonce;
- command expiry if configured;
- replay protection;
- profile permission.

### 19.7 Notification privacy

Every event passes a channel-specific redaction policy.

Organization notifications should contain only:

- task ID;
- repository identifier;
- process stage;
- status;
- decision category;
- link to the approved internal system.

They should not contain source code, secrets, customer payloads, full issue descriptions, or confidential documentation in an unapproved channel.

### 19.8 Commands

Telegram examples:

```text
/status flow-123
/details flow-123
/pause flow-123
/resume flow-123
/deploy flow-123
/rollback flow-123
/retry flow-123
/cancel flow-123
/approve approval-456
/reject approval-456
/step flow-123 review auto
/step flow-123 deploy command
```

### 19.9 Workflow-level override

```yaml
notifications:
  granularity: step-progress

  step_overrides:
    research:
      progress_interval: 15m

    build:
      progress_interval: 5m

    deploy:
      granularity: operation
      immediate: true
```

### 19.10 Telegram limits and conservative internal ceilings

Telegram's official Bot FAQ recommends avoiding more than:

```text
one message per second in one chat
20 messages per minute in a group
about 30 broadcast messages per second globally
```

The Bot API limits text messages to 4096 characters after entity parsing. Flood-control responses may include `parameters.retry_after`, which is the number of seconds the sender must wait before retrying.

Delivery Foundry uses lower internal defaults to preserve safety margin:

```yaml
telegram:
  limits:
    mode: adaptive

    private_chat:
      messages_per_second: 0.80
      minimum_interval: 1250ms

    group_or_supergroup:
      messages_per_minute: 15
      minimum_interval: 4s

    global_free_broadcast:
      messages_per_second: 25

    safety_margin_percent: 20

    text:
      telegram_hard_max_characters: 4096
      foundry_target_max_characters: 3500
      foundry_absolute_chunk_max_characters: 3900

    paid_broadcast:
      enabled: false
      require_explicit_budget_policy: true
```

Paid broadcasts are disabled by default. Telegram currently supports an optional paid-broadcast flag for higher broadcast throughput, but Delivery Foundry may use it only through an explicit spending policy and hard budget gate.

All outbound Telegram mutations pass through the same limiter:

```text
sendMessage
editMessageText
editMessageCaption
sendPhoto/sendDocument captions
delete or pin operations where flood control applies
```

The limiter is keyed by:

```text
bot
chat
chat type
method class
global broadcast pool
```

### 19.11 Every event is preserved, but not every event becomes a separate message

The requirement is:

```text
every process event is durably recorded
and eventually represented to the operator
```

It is not:

```text
one Telegram API request for every process event
```

Delivery pipeline:

```text
workflow event
→ durable outbox
→ priority classifier
→ aggregation key
→ coalescing window
→ edit existing step message OR create digest
→ rate-limit scheduler
→ Telegram
→ delivery receipt
```

Every event remains individually queryable in the Foundry event store even when twenty events are represented by one Telegram digest.

### 19.12 Priority lanes

| Priority | Examples | Delivery behavior |
|---|---|---|
| P0 Critical | Security incident, destructive rollback, credential exposure | Immediate dedicated message; never dropped |
| P1 Command | `/deploy` required, approval required, production failure | Dedicated message; delivery receipt required |
| P2 State | Step started/completed/failed, provider failover, checkpoint | Coalesce briefly; preserve every transition in digest |
| P3 Progress | Percent complete, repeated test progress, routine heartbeat | Prefer edit; batch aggressively |
| P4 Trace | Tool calls and low-level operations | Store durably; send only in operation/debug mode |

P0 and P1 may bypass the normal coalescing delay, but they never bypass Telegram rate limiting.

When a P0 or P1 event arrives under rate pressure:

```text
preempt queued P3/P4 traffic
→ deliver critical event at next permitted slot
→ include count of deferred low-priority events
```

### 19.13 Adaptive batching

Default:

```yaml
telegram:
  batching:
    enabled: true

    aggregation_key:
      - chat_id
      - workflow_id
      - step_id

    coalesce_window: 3s
    maximum_batch_wait: 30s
    maximum_events_per_batch: 20
    batch_when_queue_depth_reaches: 8

    progress:
      strategy: edit-existing-message
      minimum_edit_interval: 5s
      maximum_silence: 15m

    state_transitions:
      strategy: digest
      preserve_order: true

    critical:
      strategy: dedicated-message

    overflow:
      strategy: summary-plus-artifact-link
      attach_text_report_when_allowed: true
```

Dynamic batching behavior:

```text
queue healthy
→ edit one message per active workflow step

queue growing
→ increase coalescing window
→ merge related transitions into one digest

predicted chat limit
→ stop new sends
→ convert pending progress into an edit or digest

predicted global limit
→ weighted-fair scheduling across chats
→ batch low-priority events

429 received
→ freeze affected bucket
→ merge queued events while waiting
→ resume after retry_after
```

### 19.14 Digest format

```text
📦 Delivery Foundry update — flow-123

Period: 14:03:10–14:03:28
Events combined: 12

✅ PLAN validated
▶️ Wave 2 started
✅ TASK-004 backend contract completed
✅ TASK-005 frontend client completed
🧪 Verification: 41 passed, 1 retrying
💾 Checkpoint: checkpoint-789
⏭ Next: integration test

Queue: 4 events remaining
/status flow-123
```

A batch must preserve:

- chronological order;
- terminal states;
- command requirements;
- failures and retry times;
- checkpoints;
- the final next action.

### 19.15 Message sizing and formatting

Text messages have a 4096-character Telegram limit after entities are parsed. Delivery Foundry targets 3500 characters to leave formatting and metadata headroom.

Rules:

1. Calculate rendered length after escaping and entity generation.
2. Prefer explicit Telegram entities or independently valid HTML chunks over splitting raw MarkdownV2.
3. Split on event boundaries, then paragraph boundaries, then sentence boundaries.
4. Every chunk carries:
   - workflow ID;
   - part number;
   - total parts;
   - batch ID.
5. Never split inside an escape sequence, entity, code block, URL, or command button.
6. If the report remains large:
   - send a concise summary;
   - store the full report as an artifact;
   - include an approved link or send it as a document when profile policy permits.
7. Captions use their separate, smaller Telegram limit and therefore contain only a concise summary.

Example:

```text
[flow-123 · batch-91 · 1/3]
...
```

### 19.16 Token-bucket and queue architecture

Use hierarchical rate limiting:

```text
global bot bucket
    ↓
chat-type bucket
    ↓
individual chat bucket
    ↓
priority queue
```

A notification is sent only when every required bucket has a token.

Conceptual state:

```yaml
telegram_rate_state:
  bot_id: bot-1

  global:
    capacity: 25
    refill_per_second: 25
    blocked_until: null

  chats:
    "123456":
      type: private
      capacity: 1
      refill_per_second: 0.80
      blocked_until: null

    "-10098765":
      type: supergroup
      capacity: 15
      refill_per_second: 0.25
      blocked_until: null
```

Use weighted fair queuing so one noisy workflow cannot starve command messages from another workflow.

### 19.17 Dynamic 429 retry

Telegram flood-control failures may include:

```json
{
  "ok": false,
  "error_code": 429,
  "parameters": {
    "retry_after": 17
  }
}
```

`retry_after` is authoritative.

```text
429
→ parse retry_after
→ persist attempt and response
→ set affected bucket blocked_until
→ add configurable grace and jitter
→ merge queued compatible events
→ schedule durable wake-up
→ retry unsent batch
```

Configuration:

```yaml
telegram:
  retry:
    enabled: true

    retry_after:
      authoritative: true
      grace: 1s
      jitter:
        mode: full
        maximum: 2s

    fallback_when_retry_after_missing:
      base_interval: 2s
      multiplier: 2
      maximum_interval: 15m
      jitter: full

    maximum_total_attempts: null
    maximum_consecutive_same_error: 5
    maximum_immediate_attempts: 1
    maximum_delivery_age: null

    strategy_after_same_error_limit:
      - increase-batch-window
      - reduce-send-rate
      - refresh-bot-health
      - switch-fallback-channel-if-configured
      - continue-durable-scheduling

    retryable:
      - 429
      - 500
      - 502
      - 503
      - 504
      - network-timeout
      - connection-reset

    non_retryable:
      - invalid-token
      - bot-blocked-by-user
      - chat-not-found
      - forbidden
      - malformed-message
```

`maximum_total_attempts: null` keeps the notification alive indefinitely. It does not create a hot loop because immediate retries, same-error attempts, intervals, and wake-ups remain bounded.

### 19.18 Dynamic calibration and self-learning

The Telegram controller learns observed safe throughput per bot and chat.

Inputs:

```text
successful sends and edits
429 responses
retry_after duration
chat type
message method
queue depth
latency
time of day
batch size
```

Adaptation:

```text
429 or rising error rate
→ reduce rate immediately
→ widen coalescing window
→ increase digest size
→ decrease progress-edit frequency

clean window for a configured duration
→ cautiously increase throughput
→ never exceed configured or official ceiling
```

Example:

```yaml
telegram:
  calibration:
    enabled: true
    observation_window: 15m
    clean_windows_before_increase: 4
    multiplicative_decrease: 0.70
    additive_increase_per_window: 0.05
    minimum_private_rate: 0.20
    maximum_private_rate: 0.80
    minimum_global_rate: 5
    maximum_global_rate: 25
    learned_state_ttl: 7d
```

Learned state is advisory, scoped to the bot/chat, expiring, auditable, and rollback-capable.

### 19.19 Notification failure and fallback

```yaml
telegram:
  fallback:
    enabled: true

    after_unavailable_for: 30m
    channels:
      - email
      - slack

    critical_immediate_fallback: true

    telegram_recovery:
      replay_summary: true
      maximum_replay_messages: 5
```

If Telegram is unavailable:

1. Continue writing every event to the outbox.
2. Batch pending events rather than spawning unlimited messages.
3. Send critical events to an approved fallback channel.
4. Keep retrying Telegram through durable scheduling.
5. On recovery, send a bounded recovery digest—not the entire historical queue as individual messages.

A `command` step cannot proceed unless:

- its Telegram command request has a confirmed delivery receipt; or
- another explicitly authorized command channel is active.

### 19.20 Telegram notification health states

```text
HEALTHY
CONSTRAINED
FLOOD_WAIT
DEGRADED
UNAVAILABLE
RECOVERING
```

Example transition:

```text
HEALTHY
→ first 429
FLOOD_WAIT
→ retry_after elapsed
RECOVERING
→ four clean windows
HEALTHY
```

### 19.21 Telegram batching tests

Required tests:

```text
1 event
→ one message

100 progress events for one step
→ one initial message plus bounded edits/digests

30 state events in one private chat
→ no configured one-chat rate violation

30 events in one group
→ no configured group-rate violation

1000 events across many chats
→ no configured global-rate violation
→ fair delivery across chats

4097-character rendered message
→ valid ordered chunks

Markdown/HTML/code-block boundary
→ no malformed chunk

429 with retry_after=17
→ no retry before blocked_until
→ compatible events batched while waiting

Telegram unavailable for one hour
→ events remain durable
→ fallback receives critical summary
→ bounded recovery digest after restoration

duplicate worker delivery
→ one logical event representation

command-required event under queue pressure
→ progress traffic preempted
→ command message delivered and acknowledged
```

### 19.22 Telegram Makefile surface

```text
  make telegram-status
  make telegram-health
  make telegram-limits
  make telegram-queue
  make telegram-batches
  make telegram-batch-flush CHAT=<chat-id>
  make telegram-retry EVENT=<event-id>
  make telegram-retry-all
  make telegram-calibrate
  make telegram-calibration-reset
  make telegram-simulate-429 RETRY_AFTER=17
  make telegram-load-test EVENTS=1000 CHATS=50
  make telegram-dead-letter
  make telegram-replay-summary
```

Example:

```text
Telegram notification controller

State: CONSTRAINED
Private-chat effective rate: 0.56 msg/s
Global effective rate: 17.5 msg/s
Queue: 84 events
Pending batches: 7
Oldest event: 42s
429 in last 15m: 2
Blocked chats: 1
Next wake: 8s
Fallback: healthy

All events durable: PASS
Command delivery lane: PASS
```

---


