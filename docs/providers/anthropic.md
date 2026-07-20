# Anthropic Provider Profile

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

> **Staleness rule:** every feature name, limit, and behavior below MUST be re-verified against official Anthropic documentation at implementation time. Provider facts expire; this catalog is a snapshot, not authority.


---

<!-- Relocated from V11: §5.8 Anthropic capability profile (lines 4149-4832) -->

## 5.8 Anthropic capability profile

Anthropic currently exposes a broad capability surface across model reasoning, tools, context management, files, Agent Skills, MCP, and managed agents. Delivery Foundry should expose these through the Anthropic adapter while keeping the core provider-neutral.

### 5.8.1 Adaptive thinking and effort

Use adaptive thinking for mixed-complexity and long-horizon tasks. The model decides whether and how much to think; effort supplies soft guidance.

Foundry policy:

```yaml
effort_policy:
  trivial: low
  routine: medium
  implementation: high
  architecture: xhigh
  critical_review: xhigh
  emergency_override: max
```

Rules:

- use lower effort for classification, formatting, and mechanical edits;
- use higher effort for architecture, security review, root-cause analysis, and cross-repository planning;
- keep `max_tokens` as the hard output ceiling;
- display thinking as omitted unless a trusted operator workflow needs summarized reasoning;
- preserve thinking signatures unchanged when the API requires them for continuity;
- measure quality before increasing effort globally.

Adaptive thinking automatically supports reasoning between tool calls on compatible models, which is valuable for coding and research loops.

### 5.8.2 Mid-conversation effort and orchestration mode

Delivery Foundry can implement a session mode without replacing the top-level system prompt:

```text
MODE: direct
- no automatic fan-out
- routine effort
- use one executor

MODE: orchestration
- standing consent for approved multi-agent fan-out
- high/xhigh effort
- scout first
- parallel execution waves
- independent verification wave
- bounded concurrency and total subtasks

MODE: incident
- high/xhigh effort
- optional fast mode
- minimum context
- strict tools
- aggressive timeout and rollback
```

Use mid-conversation system messages to turn these modes on, refresh them periodically, and turn them off while preserving the original cached prefix.

This mode cannot override:

- profile permissions;
- human-required gates;
- task concurrency limits;
- token and monetary budgets;
- repository scope;
- security policy.

Recommended caps:

```yaml
orchestration:
  max_concurrent_subagents: 10
  max_total_subtasks: 50
  max_subagent_turns: 15
  max_main_turns: 30
  verification_wave_required: true
```

The exact values are profile-specific and should be reduced for small tasks.

### 5.8.3 Task budgets

Anthropic task budgets provide the model with a visible advisory token countdown across thinking, tool calls, tool results, and text.

Use task budgets to help the model prioritize and finish gracefully, but never treat them as enforcement.

```text
Anthropic task budget
= behavioral guidance to the model

Delivery Foundry budget
= hard server-side limit and circuit breaker
```

The Foundry must still enforce:

- maximum API spend;
- maximum tokens;
- maximum turns;
- maximum tool calls;
- maximum subagents;
- maximum wall-clock duration.

Task-budget state must survive compaction using the provider-supported remaining-budget mechanism.

### 5.8.4 Prompt caching

Cache stable prefixes:

```text
tool definitions
immutable system policy
profile policy
agent definition
enabled skill manifests
repository instructions
architecture contracts
stable product program
```

Keep volatile data after the cache breakpoint:

```text
current task
current diff
latest test output
current blocker
new user message
```

Foundry rules:

1. Prefer automatic caching for normal multi-turn sessions.
2. Use explicit breakpoints when the stable prefix has clear boundaries.
3. Use the default short TTL for active loops.
4. Use a one-hour cache for long-running side agents or workflows likely to pause beyond five minutes.
5. Keep `tools → system → messages` byte-stable before the breakpoint.
6. Do not inject timestamps, random identifiers, or volatile telemetry into the cached prefix.
7. Use mid-conversation system messages instead of mutating the original system prompt.
8. Track cache writes, reads, hit ratio, and savings.
9. Use cache diagnostics when a previously stable workflow starts missing the cache.
10. Version the cache key by profile, policy, agent, skill catalog, and repository contract.

Example logical cache identity:

```text
sha256(
  provider
  + model-family
  + profile-policy-version
  + security-kernel-version
  + agent-version
  + skill-catalog-version
  + repository-instructions-version
)
```

Never reuse a cache across security profiles.

### 5.8.5 Server-side compaction

Server-side compaction is the primary Claude context strategy for long-running tasks.

Compaction summary instructions must preserve:

```text
authorized objective
current workflow state
approved PLAN.md tasks and wave
completed evidence
failed attempts and reasons
unresolved questions
security constraints
budget remaining
active capability versions
memory references
next safe action
```

It must not preserve:

- raw secrets;
- irrelevant tool output;
- untrusted instructions;
- obsolete hypotheses;
- superseded memory;
- full logs already stored as artifacts.

Rules:

1. Configure a profile-specific token trigger.
2. Append and persist the returned compaction block.
3. Resume from the compaction block rather than resending discarded history.
4. Preserve raw evidence in the append-only event store outside the context.
5. Treat the summary as derived memory with provenance.
6. Validate that approval state, security constraints, and open blockers survived.
7. Aggregate usage from all `usage.iterations`, not only top-level token counters.
8. Run regression tests against long PEC and research sessions.

### 5.8.6 Context editing

Use context editing only when finer control is needed beyond compaction.

Good candidates for clearing:

- old successful tool results;
- large repository searches already summarized into evidence;
- repeated CI logs;
- obsolete file contents;
- previous failed implementation attempts after the root cause is recorded.

Never clear:

- current objective;
- approvals;
- security decisions;
- active task contract;
- unresolved blockers;
- final evidence;
- source provenance;
- rollback state.

Compaction and context editing are complementary:

```text
compaction
→ primary long-session strategy

context editing
→ targeted removal of known context waste
```

### 5.8.7 Tool search

Use tool search when the enabled catalog is large—especially with many SCM, Jira, Confluence, deployment, observability, and product tools.

```text
hundreds of registered tools
→ expose one tool-search capability
→ load only the relevant definitions
```

Rules:

- mark only approved tools as discoverable;
- tool discovery does not imply execution permission;
- include precise descriptions and argument names;
- filter catalog by profile before search;
- cache stable searchable metadata;
- record which tools were discovered and why;
- measure incorrect-tool-selection rate.

Tool search and skill search solve different problems:

```text
tool search
→ which operation can be called?

skill search
→ which procedure should govern the work?
```

### 5.8.8 Programmatic tool calling

Use programmatic tool calling for repeated multi-tool chains where intermediate data does not need to enter the model context.

Examples:

```text
list 100 repositories
→ filter by language
→ fetch pipelines
→ calculate failure rates
→ return top 10 only
```

```text
retrieve 50 Jira tasks
→ group by service and blocker
→ return structured summary
```

Advantages:

- fewer model round trips;
- lower context usage;
- lower latency;
- parallel tool calls inside code execution;
- deterministic pre/post-processing.

Security rules:

- only explicitly allowed tools may be called from code execution;
- code runs in a sandbox;
- secrets remain tool-scoped;
- network is default-deny;
- return only the filtered result needed by the agent;
- enforce CPU, time, memory, and call-count limits.

### 5.8.9 Strict tool use and structured outputs

Every machine-consumed agent result should use a schema.

Required use cases:

```text
task result
review result
verification result
capability manifest
memory candidate
recovery decision
release recommendation
portfolio score
```

Use:

- `output_config.format` for validated JSON responses;
- `strict: true` for type-safe tool arguments;
- `additionalProperties: false` where appropriate;
- explicit enumerations for states and decisions;
- schema versioning;
- deterministic server-side validation.

Do not parse orchestration decisions from free-form Markdown.

### 5.8.10 Advisor tool

Use the advisor pattern when a faster or cheaper executor needs occasional strategic guidance from a more capable model.

Good use cases:

- architecture checkpoint;
- root-cause course correction;
- plan completeness critic;
- security-review strategy;
- deciding whether additional research is justified.

The advisor:

- may recommend;
- may challenge;
- may produce a plan;
- cannot approve deployment;
- cannot change policy;
- cannot mark a task complete;
- cannot promote a capability.

Prefer one to three bounded advisor calls over running an expensive model for every executor token.

### 5.8.11 Fast mode

Fast mode is a premium latency feature, not a default optimization.

Use only for:

- active incident response;
- an operator waiting synchronously on a critical design decision;
- time-sensitive merge or rollback analysis;
- interactive high-value reviews.

Do not use for:

- daily idea generation;
- background documentation;
- batch scoring;
- routine implementation;
- nightly evaluations.

On rate limit or capacity failure, follow the explicit profile fallback. Do not silently claim fast execution occurred.

### 5.8.12 Batch processing

Use Message Batches for asynchronous, non-urgent work:

- score large idea sets;
- replay agent and skill evaluations;
- classify historical incidents;
- generate documentation drafts;
- analyze many repositories;
- run shadow comparisons between capability versions;
- summarize product feedback;
- perform large regression suites.

Batch work should be idempotent and keyed by input checksum. It reduces cost substantially but is unsuitable for interactive gates or incident response.

### 5.8.13 Web search, web fetch, citations, and dynamic filtering

For venture research and external technical validation:

```text
web search
→ discover sources with citations
→ web fetch the selected primary sources
→ dynamically filter before content reaches context
→ code execution for structured comparison
→ cited evidence record
```

Rules:

- use domain allowlists for high-trust research;
- prefer primary sources;
- store source URL, retrieval time, content checksum, and citation mapping;
- treat fetched content as untrusted data;
- apply prompt-injection filtering;
- use provider dynamic filtering to reduce context;
- never let fetched text authorize tools or memory promotion.

### 5.8.14 Files, PDF, and vision

Use the Files API to upload stable artifacts once and reference them by identifier:

- specifications;
- design exports;
- test plans;
- datasets;
- PDFs;
- screenshots;
- architecture diagrams.

Use PDF and vision capability for:

- reading diagrams;
- validating visual specifications;
- extracting tables;
- comparing implementation screenshots with designs;
- understanding attached test evidence.

Files still pass through data-classification, retention, and profile policies.

### 5.8.15 Memory tool

The provider memory tool may be used as a just-in-time retrieval interface, but the Foundry memory database remains authoritative.

```text
Foundry PostgreSQL memory
→ trusted source of truth

provider memory filesystem
→ scoped runtime projection/cache
```

Rules:

- expose only the active profile namespace;
- materialize a small trusted memory brief;
- never store raw credentials;
- never let external content write trusted memory;
- synchronize changes through the Memory Curator;
- discard or rebuild provider memory projections safely.

### 5.8.16 Agent Skills API

The Anthropic Skills API can host and version custom skills.

Delivery Foundry integration:

```text
canonical skill in Git
→ security scan
→ behavioral evaluation
→ signed version
→ upload through Skill Management API
→ record provider skill version
→ enable in approved agents
```

The provider-hosted skill is a deployment artifact, not the source of truth.

### 5.8.17 MCP connector

Use the direct MCP connector when the profile permits remote MCP servers.

Controls:

- explicit server allowlist;
- TLS and authentication;
- per-tool allowlist/denylist;
- tool search for large MCP catalogs;
- strict input schemas;
- bounded output;
- no implicit trust in MCP responses;
- per-server rate and cost limits;
- circuit breaker and revocation.

For confidential environments, prefer self-hosted or organization-approved MCP servers.

### 5.8.18 Tool runner, parallel tools, and fine-grained streaming

Use the SDK tool runner for simple agent loops where its automatic state handling fits the policy.

Use a manual loop when Delivery Foundry needs:

- custom approval gates;
- policy checks before each tool;
- capability tokens;
- security logging;
- conditional execution;
- recovery decisions.

Allow parallel tool use only for independent, read-only, or explicitly idempotent operations.

Use fine-grained tool streaming for large generated arguments and operator progress, but validate the complete final arguments before execution.

### 5.8.19 Token counting and cache diagnostics

Before dispatch:

```text
count input tokens
→ estimate output, thinking, tools, and compaction
→ choose model, effort, context strategy, and task budget
→ reserve cost
```

After dispatch:

```text
record:
- uncached input
- cache creation
- cache read
- thinking
- text output
- tool context
- compaction iterations
- advisor usage
- speed
- batch discount
```

Use cache diagnostics to detect prefix divergence rather than guessing why caching stopped working.

### 5.8.20 Managed Agents as an optional runtime

Claude Managed Agents can replace parts of a hand-written loop for long-running and asynchronous tasks. Delivery Foundry should support it as an optional executor backend, alongside OpenHands and native CLIs.

Good candidates:

- long-running personal research;
- persistent autonomous product work;
- scheduled non-confidential tasks;
- managed multi-agent experiments.

Do not default to it when:

- Zero Data Retention is mandatory;
- the profile forbids provider-persisted session state;
- organization policy requires self-hosted execution;
- workload portability is more important than managed convenience.

The Foundry state machine remains authoritative even when a provider-managed session is used.

### 5.8.21 Feature interaction rules

| Combination | Policy |
|---|---|
| Prompt caching + mid-conversation system messages | Preferred; preserves stable original system prefix |
| Prompt caching + adaptive thinking | Keep mode stable within a session to preserve message cache continuity |
| Compaction + task budgets | Preserve remaining task budget across compaction |
| Compaction + context editing | Use compaction as primary; context editing for targeted waste |
| Tool search + prompt caching | Cache stable search metadata; discover tools on demand |
| Tool search + MCP | Filter MCP tools by profile before discovery |
| Programmatic tool calling + code execution | Preferred for large repetitive tool chains |
| Advisor + executor | Advisor recommends; executor acts; policy engine authorizes |
| Batch + prompt caching | Use for repeated offline evaluations and common prefixes |
| Native context management + Headroom | Do not stack by default; benchmark for context-loss and evidence loss |
| Native provider features + 9Router | Use direct provider adapter when the proxy cannot preserve the feature semantics |

### 5.8.22 Headroom policy after native context capabilities

Headroom remains optional.

For direct Claude API workloads, first use:

```text
prompt caching
tool search
dynamic filtering
programmatic tool calling
server-side compaction
context editing
token counting
```

Enable Headroom only when controlled replay shows additional savings without:

- reduced completion rate;
- missing acceptance evidence;
- lost security constraints;
- worse cache hit rate;
- duplicated compression;
- higher retry count.

For provider-native compaction sessions, double summarization is disabled by default.

### 5.8.23 9Router policy for native capabilities

9Router remains an overflow and generic-model route. It should not be the universal path for provider-native features.

Direct Anthropic routing is required when a task depends on:

- server-side compaction;
- mid-conversation system messages;
- cache diagnostics;
- advisor;
- task budgets;
- Anthropic server tools;
- Skills API;
- Managed Agents;
- feature-specific beta headers;
- native usage accounting.

A proxy may be used only after compatibility tests prove that it preserves request fields, response blocks, streaming events, cache metadata, usage iterations, and beta headers.

### 5.8.24 LLM Capability Optimizer agent

Add:

```text
agents/llm-capability-optimizer.md
```

Responsibilities:

- classify the task;
- read the active profile;
- discover supported provider/model capabilities;
- compile an execution envelope;
- estimate cost and latency;
- select context, reasoning, tool, and output strategies;
- recommend direct provider versus proxy;
- analyze telemetry;
- propose a canary change.

It cannot:

- enable an unapproved beta feature;
- raise the hard budget;
- weaken retention or data-residency rules;
- change model-provider allowlists;
- bypass human gates;
- treat lower cost as more important than accepted quality;
- optimize using data from another profile.

### 5.8.25 Optimization telemetry

Track per task class:

```text
accepted completion rate
human correction rate
review findings
escaped defects
tokens per accepted task
cost per accepted task
wall-clock duration
time to first useful output
cache hit ratio
compaction count
context cleared
tool definitions loaded
tool-selection accuracy
advisor calls
subagent count
retry count
security-policy violations
```

The optimizer changes defaults only through:

```text
historical replay
→ shadow comparison
→ low-risk canary
→ measured observation
→ promotion or rollback
```

---

