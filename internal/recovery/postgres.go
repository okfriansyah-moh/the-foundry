// postgres.go: docs/PLAN.md Task 94 (FND-13R)'s live ProjectionSource —
// see supervisor.go's own doc comment for why this adapter was
// deliberately deferred out of Task 32 and is only being built now, once
// this task's live Postgres+Temporal environment exists to verify it
// against.
package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// PostgresProjectionSource implements ProjectionSource by reading Task
// 14's workflow_status_projection directly. It never writes — this
// mirrors the projection's own Constitution C3 contract (a rebuildable
// read model, not authority).
type PostgresProjectionSource struct {
	DB *sql.DB
}

// ListNonterminal returns one WorkflowSnapshot per row whose status is
// not one of the three terminal statuses (state.Status.IsTerminal).
// updated_at is used as LastProgressAt (this task's Scope note: a
// sufficient proxy given no finer-grained checkpoint timestamp exists in
// this table); LastHeartbeat is left zero — only a
// CompositeProjectionSource wrapping this with a Temporal heartbeat
// source fills it in. RecentFailures is left nil (documented gap: no
// caller in this repo populates failure-signature history yet).
func (s *PostgresProjectionSource) ListNonterminal(ctx context.Context) ([]WorkflowSnapshot, error) {
	const q = `
SELECT workflow_id, status, reason, attempt, wake_at, updated_at
FROM workflow_status_projection
WHERE status NOT IN ($1, $2, $3)`

	rows, err := s.DB.QueryContext(ctx, q,
		string(state.StatusSucceeded), string(state.StatusFailed), string(state.StatusCancelled))
	if err != nil {
		return nil, fmt.Errorf("recovery: list nonterminal projections: %w", err)
	}
	defer rows.Close()

	var snaps []WorkflowSnapshot
	for rows.Next() {
		var (
			workflowID string
			status     sql.NullString
			reason     sql.NullString
			attempt    sql.NullInt32
			wakeAt     sql.NullTime
			updatedAt  time.Time
		)
		if err := rows.Scan(&workflowID, &status, &reason, &attempt, &wakeAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("recovery: scan nonterminal projection row: %w", err)
		}
		snap := WorkflowSnapshot{
			WorkflowID:     workflowID,
			Status:         state.Status(status.String),
			Reason:         state.Reason(reason.String),
			Attempt:        int(attempt.Int32),
			LastProgressAt: updatedAt,
		}
		if wakeAt.Valid {
			t := wakeAt.Time
			snap.WakeAt = &t
		}
		snaps = append(snaps, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recovery: iterate nonterminal projections: %w", err)
	}
	return snaps, nil
}
