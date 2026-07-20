# Capacity and Provider Awareness

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.**


---

<!-- Relocated from V11: §20.7 Provider-capacity awareness (lines 12059-12397) -->

## 20.7 Provider-capacity awareness

Delivery Foundry must treat model capacity as a first-class scheduling resource.

The scheduler tracks six different limits because they fail differently:

| Capacity class | Examples |
|---|---|
| Request rate | Requests per minute, concurrent requests |
| Token throughput | Input tokens per minute, output tokens per minute |
| Context capacity | Context-window size, current context usage, compaction headroom |
| Subscription allowance | Rolling usage windows, monthly AI credits, premium requests |
| Financial capacity | API balance, workspace spend limit, product budget |
| Runtime capacity | Concurrent cloud agents, local VRAM/RAM, sandbox slots |

Do not call every problem a “token limit.” The recovery strategy depends on the exact capacity class.

### 20.7.1 Capacity Broker

Capacity management is deterministic infrastructure, not an LLM decision.

```text
task estimate
    ↓
Capacity Broker
    ├── current provider limits
    ├── observed remaining capacity
    ├── subscription reset windows
    ├── active reservations
    ├── provider health
    ├── profile permissions
    └── hard budget
    ↓
admit, resize, reroute, queue, or wait
```

The optional `capacity-analyst` agent may explain patterns and propose tuning, but it cannot reserve capacity, override a limit, or authorize spend.

### 20.7.2 Capacity snapshot

```yaml
snapshot_id: cap-20260718-001
provider: anthropic
surface: api
model_group: sonnet

observed_at: 2026-07-18T12:00:00Z
source: response-headers
confidence: exact

limits:
  requests_per_minute: 1000
  input_tokens_per_minute: 2000000
  output_tokens_per_minute: 400000

remaining:
  requests: 842
  input_tokens: 1530000
  output_tokens: 318000

reset_at:
  requests: 2026-07-18T12:00:01Z
  input_tokens: 2026-07-18T12:00:01Z
  output_tokens: 2026-07-18T12:00:01Z

financial:
  spend_remaining_usd: 214.30

health:
  state: healthy
  recent_429_rate: 0.01
  recent_5xx_rate: 0.002
  latency_p95_ms: 4800
```

Exact numeric limits must be discovered dynamically. They must not be copied from documentation into permanent configuration.

### 20.7.3 Capacity-source hierarchy

Use the strongest available signal:

```text
1. Provider rate-limit or usage API
2. Response rate-limit headers
3. Structured CLI usage/status output
4. Provider error response with reset time
5. Billing/usage integration
6. Conservative observation-based estimate
7. Unknown capacity
```

Unknown capacity is not interpreted as unlimited.

### 20.7.4 Provider-specific observations

#### Anthropic API

The adapter reads:

```text
retry-after
anthropic-ratelimit-requests-*
anthropic-ratelimit-tokens-*
anthropic-ratelimit-input-tokens-*
anthropic-ratelimit-output-tokens-*
```

Where organization access permits, it periodically synchronizes configured groups through the Rate Limits API rather than hardcoding model limits.

Prompt caching is included in capacity planning because cached input generally consumes less effective input-token throughput than uncached repeated context.

#### Claude Code subscription

Subscription usage is an opaque rolling allowance rather than a normal API token bucket.

The adapter should use native status/usage commands where available and classify messages such as:

```text
limit reached
resets at <time>
usage window exhausted
weekly limit reached
```

When the reset time is known:

```text
checkpoint
→ set wake_at to reset time + safety grace
→ release workspace lease if safe
→ resume automatically
```

When the reset time is unknown:

```text
checkpoint
→ exponential polling with a long ceiling
→ preserve the workflow as WAITING_FOR_CAPACITY
```

The workflow may fall back to approved API billing only when the active profile permits it.

#### OpenAI API

The adapter reads the request and token rate-limit headers, logs request IDs, and handles HTTP 429 with reset-aware or jittered exponential backoff.

The task estimator must avoid oversized completion reservations and must separate rate-limit failure from exhausted monthly budget.

#### Codex subscription

Codex usage varies with task complexity, execution surface, context size, and plan allowance. Treat subscription capacity as an observed quota pool.

Fallback order may be:

```text
Codex included usage
→ configured Codex credits
→ approved OpenAI API
→ another approved coding executor
→ wait for reset
```

The system must not purchase or consume extra credits unless the budget policy authorizes it.

#### Cursor

Track:

```text
first-party model pool
API-priced usage pool
background-agent spend limit
monthly reset
observed limit messages
```

If no stable machine-readable usage endpoint exists for the selected surface, use a conservative learned capacity model and the provider’s explicit limit/error notifications.

#### GitHub Copilot

Track both:

```text
temporary service rate limiting
AI-credit or premium-usage allowance
additional-usage budget
```

A temporary rate limit should wait and retry. An exhausted allowance should route to an included model, authorized additional usage, another provider, or the next reset window according to policy.

#### Local runtimes

Capacity includes:

```text
VRAM
RAM
CPU
context window
queued jobs
thermal or process health
disk
```

Local capacity is not infinite and needs the same reservation and circuit-breaker behavior.

### 20.7.5 Capacity reservation

Before dispatch:

```text
estimate input
estimate output
estimate tool-result volume
estimate thinking/effort
estimate number of turns
estimate subagent fan-out
estimate cached versus uncached input
reserve capacity
```

Reservation:

```yaml
reservation_id: reserve-123
workflow_id: flow-456
task_id: TASK-014

provider: anthropic
model_group: sonnet

estimated:
  requests: 12
  uncached_input_tokens: 180000
  cached_input_tokens: 420000
  output_tokens: 45000
  cost_usd: 2.40
  wall_clock: 25m

safety_margin:
  tokens_percent: 20
  requests_percent: 10

expires_at: 2026-07-18T12:30:00Z
status: active
```

If capacity cannot be reserved:

```text
shrink task
compact context
reduce parallelism
select a lower-capacity model where quality permits
route to another approved pool
submit as batch
queue until reset
```

Never begin ten parallel agents merely because ten tasks exist.

### 20.7.6 Capacity-aware concurrency

```text
available concurrency
=
min(
  provider request headroom,
  provider token headroom,
  sandbox slots,
  repository conflict limit,
  budget headroom,
  configured maximum
)
```

Concurrency is recalculated after every response and capacity event.

When input-token headroom is low:

- prioritize cached-prefix work;
- avoid large uncached repository scans;
- run programmatic filtering;
- delay context-heavy planning;
- continue low-context mechanical tasks.

When output-token headroom is low:

- reduce concurrent implementation/review streams;
- require concise structured output;
- defer long documentation generation;
- use a provider/model with separate headroom.

### 20.7.7 Pre-exhaustion behavior

Do not wait for a hard failure.

Warning thresholds:

```yaml
capacity_thresholds:
  healthy_above_percent: 40
  constrained_below_percent: 25
  drain_below_percent: 10
  stop_admission_below_percent: 5
```

At `constrained`:

- reduce new concurrency;
- compact or clear unrelated sessions;
- choose smaller task packets;
- switch noncritical work to batch;
- preserve headroom for verification and rollback.

At `drain`:

- stop admitting new long tasks;
- finish or checkpoint existing work;
- reserve capacity for state persistence and final evidence;
- calculate the next reset or fallback.

### 20.7.8 Provider health score

```text
health score =
capacity headroom
× success rate
× latency score
× verification acceptance rate
× cost efficiency
× security eligibility
```

Security eligibility is binary. A cheap provider forbidden by the profile receives a score of zero.

---



---

<!-- Relocated from V11: §20.12 Capacity-aware self-learning (lines 12839-12950) -->

## 20.12 Capacity-aware self-learning

Delivery Foundry learns how much capacity tasks actually consume.

Dimensions:

```text
provider
model
task class
repository size
changed-file count
effort
tool catalog size
cache hit ratio
compaction count
subagent count
input/output tokens
cost
wall clock
accepted result
```

### 20.12.1 Forecasting

Use simple robust statistics before complex machine learning:

```text
median
P75
P90
exponentially weighted moving average
recent failure rate
```

Forecast example:

```yaml
task_class: backend-small-feature
provider: codex-subscription

observations: 42
predicted:
  usage_units_p50: 0.7
  usage_units_p90: 1.4
  duration_p90: 28m
  retry_probability: 0.12
  acceptance_probability: 0.91

recommendation:
  reserve_multiplier: 1.3
  max_parallel: 3
```

### 20.12.2 Learned avoidance

The system can adapt:

- task size before dispatch;
- concurrency;
- provider selection;
- cache usage;
- compaction threshold;
- model effort;
- batch versus synchronous execution;
- provider-reset scheduling;
- fallback order;
- safety margin.

It cannot adapt:

- hard budgets upward;
- provider security allowlists;
- human-required gates;
- data classification;
- retry frequency beyond safety limits;
- policy to purchase credits;
- authorization to use personal subscriptions for organization data.

### 20.12.3 Capacity memory

Capacity observations are stored as scoped operational memory.

They must expire because provider limits and products change.

```yaml
memory:
  type: capacity-model
  scope: personal-github
  provider: cursor
  expires_after: 14d
  confidence: observed
  sources:
    - 37 workflow runs
```

### 20.12.4 Adaptive preemption

For low-priority work:

```text
capacity drops below drain threshold
→ checkpoint task
→ release provider slot
→ preserve reservation metadata
→ allow high-priority verification/rollback
→ resume after reset
```

Preemption is permitted only at safe task boundaries or after a valid checkpoint.


