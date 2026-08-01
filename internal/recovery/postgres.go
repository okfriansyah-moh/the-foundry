// postgres.go: docs/PLAN.md Task 94 (FND-13R)'s live ProjectionSource —
// built once Task 94's live Postgres+Temporal environment existed to verify
// it against (Task 32 delivered Classify/Supervisor; Task 94 wired foundryd).
package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
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
// source fills it in. RecentFailures is populated from
// task_failure_signatures (docs/PLAN.md Task 123): the current task's
// failure-signature history, oldest first, bounded to recentFailureWindow —
// the data the supervisor's PoisonedTask condition classifies against.
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
	defer func() { _ = rows.Close() }()

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

	// Second pass: populate each snapshot's RecentFailures from the
	// failure-signature history the kernel's runTask writes. Done per workflow
	// (the nonterminal set is small) after the primary cursor is closed, so the
	// two queries never share a connection mid-iteration.
	for i := range snaps {
		failures, ferr := s.recentFailures(ctx, snaps[i].WorkflowID)
		if ferr != nil {
			return nil, ferr
		}
		snaps[i].RecentFailures = failures
	}
	return snaps, nil
}

// recentFailureWindow bounds how many of a workflow's most recent failure
// signatures are loaded — enough for the supervisor's PoisonedTask "last two
// identical" check plus headroom, never the whole unbounded history.
const recentFailureWindow = 8

// recentFailures loads the current task's failure-signature history for
// workflowID, oldest first. The "current task" is the task_id with the most
// recent signature, so a workflow that has moved past a failing task is judged
// on its live task, not a resolved one (docs/PLAN.md Task 123).
func (s *PostgresProjectionSource) recentFailures(ctx context.Context, workflowID string) ([]FailureSignature, error) {
	const q = `
SELECT classification, detail_digest
FROM task_failure_signatures
WHERE workflow_id = $1
  AND task_id = (
        SELECT task_id FROM task_failure_signatures
        WHERE workflow_id = $1
        ORDER BY occurred_at DESC
        LIMIT 1
      )
ORDER BY occurred_at ASC
LIMIT $2`
	rows, err := s.DB.QueryContext(ctx, q, workflowID, recentFailureWindow)
	if err != nil {
		return nil, fmt.Errorf("recovery: load failure signatures for %s: %w", workflowID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []FailureSignature
	for rows.Next() {
		var classification, detail string
		if err := rows.Scan(&classification, &detail); err != nil {
			return nil, fmt.Errorf("recovery: scan failure signature for %s: %w", workflowID, err)
		}
		out = append(out, FailureSignature{
			Classification: verify.Classification(classification),
			Detail:         detail,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recovery: iterate failure signatures for %s: %w", workflowID, err)
	}
	return out, nil
}
