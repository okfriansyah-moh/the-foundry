# Runbook: ProjectionLagHigh

Alert: `deploy/prometheus/alerts.yml` — `foundry_projection_lag_seconds > 60` for 5m.

Metric: `foundry_projection_lag_seconds` (`internal/observe/metrics.go`), set by
`internal/projection.Projector.Tick` from the most recently projected
transition's `OccurredAt` (`internal/projection/projector.go`).

## What this means

`workflow_status_projection` — the rebuildable, versioned read model of workflow
status (Constitution C3; `docs/foundry/docs/architecture/data-consistency.md` §2)
— is falling behind the `workflow_transitions` stream the kernel appends to.
This is a **read-model staleness** signal, not a kernel/execution problem:
workflows keep running and transitions keep being durably recorded regardless
of projection lag (data-consistency.md §3, "PostgreSQL outage: workflows
continue executing; projections and ledgers catch up from offsets").

No control decision anywhere reads this projection to decide anything
(Constitution C4/C3) — the immediate risk is stale **operator/API visibility**
(`GET /v1/workflows/{id}/status?consistency=projected`, `foundry status`), not
correctness of the system itself. Clients needing a guaranteed-current view
can request `?consistency=fresh` (reads Temporal directly) or `foundry status
--fresh` while this alert is open.

## Triage

1. **Confirm the read-model gap is real, not one workflow's clock skew:**
   ```sql
   SELECT workflow_id, updated_at, now() - updated_at AS lag
   FROM workflow_status_projection
   ORDER BY updated_at ASC
   LIMIT 20;
   ```
   Widespread old `updated_at` values confirms a systemic lag, not a single
   stuck workflow.

2. **Check whether the projector is running at all.** This repo does not
   (yet) run a continuously-ticking projector loop inside `foundryd`; the
   projection is refreshed by explicit `foundry projection rebuild` /
   `foundry projection rollout` invocations. Confirm the process/cron/operator
   step that is supposed to invoke one of these is actually running and not
   erroring:
   ```
   foundry projection rebuild --pg-dsn "$PG_DSN"
   ```
   A clean `PASS: projection rebuilt — rows=N checksum=...` resolves the lag
   immediately (full truncate + replay from seq 0).

3. **Check for a stuck/failed `foundry projection rollout`.** A rollout in
   progress (or one that errored mid-convergence — see
   `internal/projection/versioning.go`'s `Rollout`) leaves the live table
   untouched until its atomic swap commits, so a stuck rollout does not by
   itself cause this alert — but a rollout that keeps failing to converge
   (`"rollout did not converge ... after N attempts"`) signals the live
   workflow-transitions write rate is currently high enough to starve a
   from-scratch backfill; that same write rate is a plausible cause of lag
   growing on its own.

4. **Check Postgres health/connectivity.** `SELECT count(*) FROM
   workflow_transitions WHERE seq > (SELECT last_seq FROM projection_offsets
   WHERE projector = 'workflow_status_projection');` — a large and growing
   number with no projector process consuming it confirms the backlog;
   check Postgres CPU/IO and connection-pool exhaustion.

5. **Verify recovery.** After running rebuild/rollout, `foundry_projection_lag_seconds`
   should drop below 60s within one scrape interval; confirm via
   `curl :9091/api/v1/query?query=foundry_projection_lag_seconds` (or the
   Grafana dashboard provisioned by `deploy/grafana/`).

## Escalation

If lag persists after a successful rebuild (i.e. it climbs again
immediately), the write rate into `workflow_transitions` likely exceeds
whatever process is expected to run `foundry projection rebuild`/`rollout`
on a schedule — escalate to wire a scheduled/continuous invocation (or the
`Projector.Run` polling loop already implemented in
`internal/projection/projector.go`) into an operational process, since none
currently runs by default.
