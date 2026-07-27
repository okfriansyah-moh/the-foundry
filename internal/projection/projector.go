package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

const (
	// ProjectorVersion is recorded on every projected row (data-consistency.md
	// §2: "versioned projector ... projector version recorded per row").
	// Bump it whenever projection semantics change, per the deploy-alongside/
	// backfill/cut-over rule in the same section.
	ProjectorVersion = "v0"

	// DefaultProjectorName is this projector's key in projection_offsets.
	DefaultProjectorName = "workflow_status_projection"

	// DefaultTable is the live projection table Tick upserts into when
	// Table is unset. The only other permitted value is ShadowTable
	// (versioning.go) — see upsertSQL's allowlist.
	DefaultTable = "workflow_status_projection"

	// DefaultBatchSize bounds how many transitions a single Tick processes.
	DefaultBatchSize = 500
)

const (
	selectTransitionsSQL = `
SELECT workflow_id, seq, payload
FROM workflow_transitions
WHERE seq > $1
ORDER BY seq
LIMIT $2`

	currentOffsetSQL = `SELECT last_seq FROM projection_offsets WHERE projector = $1`

	advanceOffsetSQL = `
INSERT INTO projection_offsets (projector, last_seq)
VALUES ($1, $2)
ON CONFLICT (projector) DO UPDATE SET last_seq = EXCLUDED.last_seq
WHERE projection_offsets.last_seq < EXCLUDED.last_seq`

	// upsertProjectionSQL is the crux of the whole task: the WHERE guard on
	// the ON CONFLICT DO UPDATE makes the write a no-op unless the incoming
	// row is semantically newer than the one already stored — out-of-order
	// and duplicate transition delivery can never regress the projected
	// state (data-consistency.md §2: "writes are idempotent").
	//
	// Fixed by a real, live-reproduced bug found by Task 39 (FND-20, M1
	// exit drill; docs/notes/m1-exit-report.md): the guard originally
	// compared `last_seq` alone. `last_seq` is a pure sequence-monotonicity
	// check — it stops an exact-duplicate seq from being reprocessed, but
	// says nothing about whether the *content* at a new, higher seq is
	// actually chronologically newer. A stale transition re-appended at a
	// later seq (e.g. a delayed backfill/replay tool inserting historical
	// data out of band) carries an *older* `occurred_at`, and the old
	// last_seq-only guard let it win, regressing `phase` backward — exactly
	// what Task 14's Acceptance ("out-of-order/duplicate seq handled
	// idempotently") requires this guard to prevent. The kernel's real
	// per-workflow append path (internal/kernel/workflow.go's
	// appendTransition, called synchronously with MaximumAttempts:1 — no
	// activity-level retry) does not itself produce this pattern in normal
	// operation, but Task 14's Acceptance commits to the idempotency
	// property unconditionally, and future replay/backfill tooling (the
	// very case Task 38's own Rollout replays from seq 0) could still
	// present a transition stream out of chronological order — so the
	// guard is fixed at the data layer rather than narrowed to match only
	// today's kernel call pattern (defense in depth over a hard-to-audit
	// negative proof).
	//
	// The WHERE clause now compares the row-value tuple
	// (EXCLUDED.occurred_at, EXCLUDED.last_seq) against the tuple already
	// stored: Postgres row-value comparison is lexicographic, so a strictly
	// newer occurred_at always wins regardless of seq, and last_seq is only
	// consulted as the tiebreaker when two transitions share the exact same
	// occurred_at (workflow.Now(ctx) can return the same instant for two
	// back-to-back activity calls). The stored side is wrapped in
	// COALESCE(..., '-infinity') because Postgres row-value comparison
	// returns NULL (never TRUE) the moment either side's leading field is
	// NULL — without this, a legacy row left NULL by this migration's ADD
	// COLUMN would never accept ANY future update, silently freezing that
	// workflow's projection forever. COALESCE makes a NULL stored
	// occurred_at sort before every real timestamp, so the first write that
	// carries one always wins, matching this guard's intended semantics.
	upsertProjectionSQL = `
INSERT INTO workflow_status_projection
    (workflow_id, status, phase, reason, result_code, attempt, checkpoint_id, wake_at, last_seq, occurred_at, projector_version, updated_at)
VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (workflow_id) DO UPDATE SET
    status            = EXCLUDED.status,
    phase             = EXCLUDED.phase,
    reason            = EXCLUDED.reason,
    result_code       = EXCLUDED.result_code,
    attempt           = EXCLUDED.attempt,
    checkpoint_id     = EXCLUDED.checkpoint_id,
    wake_at           = EXCLUDED.wake_at,
    last_seq          = EXCLUDED.last_seq,
    occurred_at       = EXCLUDED.occurred_at,
    projector_version = EXCLUDED.projector_version,
    updated_at        = now()
WHERE (EXCLUDED.occurred_at, EXCLUDED.last_seq) > (COALESCE(workflow_status_projection.occurred_at, '-infinity'), workflow_status_projection.last_seq)`

	// upsertProjectionShadowSQL is upsertProjectionSQL's byte-for-byte
	// counterpart targeting workflow_status_projection_shadow — Task 38's
	// (FND-19) versioned-rollout shadow table (see versioning.go's
	// Rollout). This is kept as a second compiled-in literal, not a
	// runtime string-interpolated table name, so the destination table
	// name is never built from a runtime value (OWASP A05 defense in
	// depth): Projector.Table only ever selects between this and
	// upsertProjectionSQL via upsertSQL()'s fixed allowlist.
	upsertProjectionShadowSQL = `
INSERT INTO workflow_status_projection_shadow
    (workflow_id, status, phase, reason, result_code, attempt, checkpoint_id, wake_at, last_seq, occurred_at, projector_version, updated_at)
VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (workflow_id) DO UPDATE SET
    status            = EXCLUDED.status,
    phase             = EXCLUDED.phase,
    reason            = EXCLUDED.reason,
    result_code       = EXCLUDED.result_code,
    attempt           = EXCLUDED.attempt,
    checkpoint_id     = EXCLUDED.checkpoint_id,
    wake_at           = EXCLUDED.wake_at,
    last_seq          = EXCLUDED.last_seq,
    occurred_at       = EXCLUDED.occurred_at,
    projector_version = EXCLUDED.projector_version,
    updated_at        = now()
WHERE (EXCLUDED.occurred_at, EXCLUDED.last_seq) > (COALESCE(workflow_status_projection_shadow.occurred_at, '-infinity'), workflow_status_projection_shadow.last_seq)`
)

// allowedProjectionTables is the fixed, closed set of destination tables
// Tick may write into, mapped to each one's compiled-in upsert statement.
// Table is only ever set internally by this package (versioning.go's
// Rollout sets it to ShadowTable for its shadow projector instance) —
// this allowlist is OWASP A05 defense in depth so an unrecognized value
// fails closed instead of ever reaching a query.
var allowedProjectionTables = map[string]string{
	DefaultTable: upsertProjectionSQL,
	ShadowTable:  upsertProjectionShadowSQL,
}

// Projector polls workflow_transitions past its recorded offset and
// idempotently upserts workflow_status_projection rows in a single
// transaction per batch (docs/PLAN.md Task 14 Step 2).
type Projector struct {
	DB *sql.DB

	// Name is this projector's projection_offsets key. Defaults to
	// DefaultProjectorName when empty.
	Name string

	// BatchSize bounds transitions processed per Tick. Defaults to
	// DefaultBatchSize when <= 0.
	BatchSize int

	// Table is the destination projection table this instance upserts
	// into. Defaults to DefaultTable (the live table) when empty. The
	// only other permitted value is ShadowTable, used by versioning.go's
	// Rollout for its shadow-table backfill — any other value fails Tick
	// closed via upsertSQL()'s allowlist.
	Table string

	// Version is the projector_version stamped on every row this
	// instance writes. Defaults to the package ProjectorVersion constant
	// when empty; versioning.go's Rollout sets this to the rollout's
	// target version for its shadow projector instance.
	Version string
}

func (p *Projector) name() string {
	if p.Name == "" {
		return DefaultProjectorName
	}
	return p.Name
}

func (p *Projector) batchSize() int {
	if p.BatchSize <= 0 {
		return DefaultBatchSize
	}
	return p.BatchSize
}

func (p *Projector) table() string {
	if p.Table == "" {
		return DefaultTable
	}
	return p.Table
}

func (p *Projector) version() string {
	if p.Version == "" {
		return ProjectorVersion
	}
	return p.Version
}

// upsertSQL resolves this instance's destination table to its compiled-in
// upsert statement, failing closed for any table not in
// allowedProjectionTables.
func (p *Projector) upsertSQL() (string, error) {
	sqlText, ok := allowedProjectionTables[p.table()]
	if !ok {
		return "", fmt.Errorf("unknown projection table %q", p.table())
	}
	return sqlText, nil
}

// Tick processes one batch of pending transitions: select rows past the
// current offset, upsert each into workflow_status_projection (guarded by
// last_seq), then advance the offset — all inside one transaction, so a
// crash mid-batch never leaves the offset ahead of what was actually
// projected (docs/PLAN.md Task 14 Step 2: "advance the offset
// transactionally with the upsert"). It returns the number of transitions
// processed.
func (p *Projector) Tick(ctx context.Context) (int, error) {
	upsertSQL, err := p.upsertSQL()
	if err != nil {
		return 0, fmt.Errorf("projection: %w", err)
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("projection: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	offset, err := currentOffset(ctx, tx, p.name())
	if err != nil {
		return 0, fmt.Errorf("projection: read offset: %w", err)
	}

	rows, err := tx.QueryContext(ctx, selectTransitionsSQL, offset, p.batchSize())
	if err != nil {
		return 0, fmt.Errorf("projection: select transitions: %w", err)
	}
	var batch []transitionRow
	for rows.Next() {
		var r transitionRow
		if err := rows.Scan(&r.WorkflowID, &r.Seq, &r.Payload); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("projection: scan transition: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("projection: iterate transitions: %w", err)
	}
	_ = rows.Close()

	if len(batch) == 0 {
		return 0, tx.Commit()
	}

	var lastSeq int64
	var lastOccurred time.Time
	for _, r := range batch {
		t, err := decodeTransition(r.Payload)
		if err != nil {
			return 0, fmt.Errorf("projection: decode transition seq %d: %w", r.Seq, err)
		}
		if err := upsertProjection(ctx, tx, upsertSQL, p.version(), r.WorkflowID, r.Seq, t); err != nil {
			return 0, fmt.Errorf("projection: upsert workflow %s seq %d: %w", r.WorkflowID, r.Seq, err)
		}
		lastSeq = r.Seq
		lastOccurred = t.OccurredAt
	}

	if err := advanceOffset(ctx, tx, p.name(), lastSeq); err != nil {
		return 0, fmt.Errorf("projection: advance offset: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("projection: commit: %w", err)
	}

	// Only the live projector's lag is meaningful for the
	// projection_lag_seconds alert (observability-and-alerts.md §1); a
	// shadow-table backfill running as part of a Task 38 rollout must
	// never overwrite that gauge with its own catch-up timing.
	if !lastOccurred.IsZero() && p.table() == DefaultTable {
		observe.SetProjectionLag(time.Since(lastOccurred).Seconds())
	}
	return len(batch), nil
}

// Run polls Tick every interval until ctx is cancelled — the projector loop
// intended to run inside foundryd (docs/PLAN.md Task 14 Step 2).
func (p *Projector) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := p.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

// transitionRow is one workflow_transitions record as read off the wire.
type transitionRow struct {
	WorkflowID string
	Seq        int64
	Payload    []byte
}

func decodeTransition(payload []byte) (state.Transition, error) {
	var t state.Transition
	if err := json.Unmarshal(payload, &t); err != nil {
		return state.Transition{}, err
	}
	return t, nil
}

func currentOffset(ctx context.Context, tx *sql.Tx, projector string) (int64, error) {
	var offset int64
	err := tx.QueryRowContext(ctx, currentOffsetSQL, projector).Scan(&offset)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return offset, nil
}

func advanceOffset(ctx context.Context, tx *sql.Tx, projector string, lastSeq int64) error {
	_, err := tx.ExecContext(ctx, advanceOffsetSQL, projector, lastSeq)
	return err
}

// upsertProjection writes the projected row for the current phase
// (Transition.PhaseTo — the phase the workflow entered by this
// transition), using sqlText (one of the two compiled-in statements
// upsertSQL resolved) and stamping version as projector_version.
func upsertProjection(ctx context.Context, tx *sql.Tx, sqlText, version, workflowID string, seq int64, t state.Transition) error {
	_, err := tx.ExecContext(ctx, sqlText,
		workflowID,
		string(t.Status),
		string(t.PhaseTo),
		string(t.Reason),
		string(t.ResultCode),
		t.Attempt,
		t.CheckpointID,
		t.WakeAt,
		seq,
		t.OccurredAt,
		version,
	)
	return err
}
