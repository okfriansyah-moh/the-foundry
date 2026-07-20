# Checkpoints, Restart, Liveness, and Disaster Recovery

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** ORPHANED is a liveness-supervisor detection condition, not a workflow status.


---

<!-- Relocated from V11: §20.8 Durable checkpoint, restart, resume (lines 12398-12582) -->

## 20.8 Durable checkpoint, restart, and resume

A process that cannot survive a terminal exit, provider limit, container restart, or model-session loss is not autonomous.

### 20.8.1 Checkpoint triggers

Create a durable checkpoint:

- before every provider call that may mutate state;
- after every accepted task milestone;
- before compaction;
- before provider failover;
- before waiting for a usage reset;
- before releasing a worktree lease;
- before restarting a sandbox;
- after a meaningful tool result;
- when the context reaches its soft threshold;
- when the liveness watchdog requests one;
- before deployment and rollback.

### 20.8.2 Provider-neutral task packet

```text
TASK-PACKET/
├── task.json
├── objective.md
├── acceptance.json
├── allowed-paths.json
├── validation.json
├── context-manifest.json
├── evidence-index.json
├── retry-history.json
├── provider-history.json
├── memory-brief.json
├── security-envelope.json
└── resume.md
```

This packet is the handoff contract between Claude, Codex, Cursor, Copilot, OpenCode, local models, and new sessions.

### 20.8.3 Session checkpoint

```yaml
checkpoint_id: checkpoint-789
workflow_id: flow-456
task_id: TASK-014
sequence: 17

repository:
  remote: github.com/example/product
  base_sha: abc123
  current_sha: def456
  worktree: workspaces/product/TASK-014
  patch_artifact: artifact://patch-789
  dirty: true

plan:
  path: docs/PLAN.md
  version: 3
  wave: 4
  task_status: implementing

completion:
  accepted_criteria:
    - AT-12
  remaining_criteria:
    - AT-13
    - AT-14

verification:
  last_command: make test-unit SCOPE=source
  last_result: passed
  evidence:
    - artifact://test-summary-111

provider:
  name: claude-code
  session_id: opaque-session-id
  model: observed-model
  context_percent: 76
  compaction_count: 1
  capacity_state: constrained

budget:
  task_remaining_usd: 1.42
  workflow_remaining_usd: 83.10

retry:
  same_error_count: 0
  total_attempts: 4
  strategy: same-provider-new-session

next_action:
  type: resume
  command: make workflow-resume WORKFLOW=flow-456
```

### 20.8.4 Atomic checkpointing

Checkpoint order:

```text
1. Persist intent to checkpoint.
2. Flush repository patch and artifacts.
3. Persist workflow state and next action.
4. Commit database transaction.
5. Acknowledge provider/tool result.
```

A crash before step 4 causes replay. External writes therefore require idempotency keys.

### 20.8.5 Restart modes

| Mode | Use |
|---|---|
| Same-session resume | Short provider throttling; context still healthy |
| Compact-and-resume | Long session nearing context limit |
| Fresh-session rollover | Too many compactions, session corruption, context quality loss |
| Same-provider model switch | Mechanical remainder can use a lighter approved model |
| Same-provider surface switch | Subscription limit → approved API surface |
| Cross-provider failover | Provider unavailable or quota exhausted |
| Batch conversion | Non-urgent bulk work |
| Local-runtime fallback | Approved and capable local model |
| Wait-until-reset | No approved fallback or budget policy forbids spending |
| Human escalation | Unsatisfied hard gate or unrecoverable ambiguity |

### 20.8.6 Fresh-session rollover

```text
checkpoint
→ close or abandon old provider session
→ create clean sandbox/session
→ load immutable policy
→ load task packet
→ restore repository state
→ run quick deterministic baseline
→ verify accepted evidence still holds
→ continue from next action
```

Do not paste the entire old conversation into the new session.

### 20.8.7 Cross-provider failover

A provider change is a controlled restart, not a live mid-sentence model switch.

Rules:

1. Stop the old execution lease.
2. Persist its final checkpoint.
3. Create a new task attempt and fencing token.
4. Give the new provider the provider-neutral task packet.
5. Require it to inspect existing changes before editing.
6. Re-run the task’s deterministic baseline.
7. Preserve accepted evidence only if still valid.
8. Require independent final verification after mixed-provider execution.

### 20.8.8 Lease, heartbeat, and fencing

Every running task has:

```text
lease_owner
lease_until
heartbeat_at
fencing_token
```

Only the current fencing token may write task state.

If heartbeat expires:

```text
watchdog marks lease stale
→ pauses external writes
→ captures sandbox if possible
→ creates recovery checkpoint
→ issues a new fencing token
→ restarts from checkpoint
```

This prevents two restarted agents from modifying the same task concurrently.

---



---

<!-- Relocated from V11: §20.10 Liveness Supervisor (lines 12698-12777) -->

## 20.10 Liveness Supervisor

The Liveness Supervisor is a deterministic daemon.

```text
scan nonterminal workflows
→ verify lease or waiting condition
→ verify checkpoint exists
→ verify next event or wake_at
→ verify retry policy
→ verify budget and security
→ repair orphaned workflows
→ notify on prolonged no-progress
```

It runs independently of the agent runtime.

### 20.10.1 Liveness invariants

For every nonterminal workflow, exactly one must be true:

```text
RUNNING with a live lease and heartbeat
WAITING with a future wake_at
WAITING for a registered external event
WAITING for a declared human gate
RECOVERING with a recovery lease
```

Anything else is an orphan.

### 20.10.2 No-progress detection

Progress is not “another model response.”

Progress means at least one of:

- acceptance criterion moved to passed;
- verified file or artifact changed;
- blocker was removed;
- task state advanced;
- failure classification improved;
- new trusted evidence was produced;
- rollback reduced blast radius;
- capacity reset became closer and scheduled.

Repeated retries with the same error produce no progress.

### 20.10.3 Stalled-workflow repair

```text
orphan detected
→ fence old worker
→ load last checkpoint
→ inspect last provider and error
→ choose restart, reroute, wait, or escalate
→ assign wake_at or live lease
→ send concise notification
```

### 20.10.4 Scheduler persistence

Use a durable database queue, not an in-memory timer.

Conceptual PostgreSQL claim:

```sql
select id
from workflow_wakeups
where wake_at <= now()
  and claimed_until < now()
order by priority desc, wake_at asc
for update skip locked
limit 1;
```

The scheduler may restart without losing timers.

---

