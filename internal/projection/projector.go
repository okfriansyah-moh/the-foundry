package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"expvar"
	"fmt"
	"time"

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

	// DefaultBatchSize bounds how many transitions a single Tick processes.
	DefaultBatchSize = 500
)

// projectionLagSeconds is exposed as a plain expvar for now (docs/PLAN.md
// Task 14 Step 4; OTel wiring is Task 31, out of scope here). It reports
// the age of the most recently projected transition's OccurredAt.
var projectionLagSeconds = expvar.NewFloat("projection_lag_seconds")

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
	// the ON CONFLICT DO UPDATE makes the write a no-op whenever the row
	// already reflects a later (or equal) seq than the one being applied —
	// out-of-order and duplicate transition delivery can never regress the
	// projected state (data-consistency.md §2: "writes are idempotent").
	upsertProjectionSQL = `
INSERT INTO workflow_status_projection
    (workflow_id, status, phase, reason, result_code, attempt, checkpoint_id, wake_at, last_seq, projector_version, updated_at)
VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (workflow_id) DO UPDATE SET
    status            = EXCLUDED.status,
    phase             = EXCLUDED.phase,
    reason            = EXCLUDED.reason,
    result_code       = EXCLUDED.result_code,
    attempt           = EXCLUDED.attempt,
    checkpoint_id     = EXCLUDED.checkpoint_id,
    wake_at           = EXCLUDED.wake_at,
    last_seq          = EXCLUDED.last_seq,
    projector_version = EXCLUDED.projector_version,
    updated_at        = now()
WHERE workflow_status_projection.last_seq < EXCLUDED.last_seq`
)

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

// Tick processes one batch of pending transitions: select rows past the
// current offset, upsert each into workflow_status_projection (guarded by
// last_seq), then advance the offset — all inside one transaction, so a
// crash mid-batch never leaves the offset ahead of what was actually
// projected (docs/PLAN.md Task 14 Step 2: "advance the offset
// transactionally with the upsert"). It returns the number of transitions
// processed.
func (p *Projector) Tick(ctx context.Context) (int, error) {
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
			rows.Close()
			return 0, fmt.Errorf("projection: scan transition: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("projection: iterate transitions: %w", err)
	}
	rows.Close()

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
		if err := upsertProjection(ctx, tx, r.WorkflowID, r.Seq, t); err != nil {
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

	if !lastOccurred.IsZero() {
		projectionLagSeconds.Set(time.Since(lastOccurred).Seconds())
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
// (Transition.PhaseTo — the phase the workflow entered by this transition).
func upsertProjection(ctx context.Context, tx *sql.Tx, workflowID string, seq int64, t state.Transition) error {
	_, err := tx.ExecContext(ctx, upsertProjectionSQL,
		workflowID,
		string(t.Status),
		string(t.PhaseTo),
		string(t.Reason),
		string(t.ResultCode),
		t.Attempt,
		t.CheckpointID,
		t.WakeAt,
		seq,
		ProjectorVersion,
	)
	return err
}
