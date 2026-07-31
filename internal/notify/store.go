package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// State is one of internal/db/migrations/00007_notifications.sql's four
// CHECK-constrained `state` values. This package never adds a fifth
// value: the dead-letter path (docs/PLAN.md Task 30 Steps) is
// represented as a terminal 'failed' row (Attempts >= max, no further
// retries scheduled) rather than a new state — see Store.MarkDeadLetter.
type State string

const (
	StatePending State = "pending"
	StateSent    State = "sent"
	StateFailed  State = "failed"
	StateAcked   State = "acked"
)

// Notification is one notifications row.
type Notification struct {
	ID        string
	Channel   string
	Target    string
	Class     string
	Payload   []byte
	State     State
	Attempts  int
	LastError string
	// NextAttemptAt is the durable not-before time for a retry
	// (docs/PLAN.md Task 112 / INT-04). A zero value means "eligible now".
	// Persisting it means Telegram's retry_after pacing survives a daemon
	// restart instead of every pending row becoming immediately eligible.
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Store is the persistence seam Engine depends on — an interface so
// unit tests can use an in-memory fake without a live Postgres
// connection, matching this task's Validation command
// ("go test ./internal/notify/... -race" with no live-infra gate).
type Store interface {
	// Enqueue durably records ev keyed by id (the notification's
	// dedupe_key). A second Enqueue for the same id is a no-op — this is
	// the idempotent-outbox-write half of at-least-once delivery
	// (docs/foundry/docs/operations/telegram.md §19.5).
	Enqueue(ctx context.Context, id, channel, target, class string, payload []byte) error
	// ClaimPending returns up to limit rows currently in StatePending,
	// oldest first.
	ClaimPending(ctx context.Context, limit int) ([]Notification, error)
	// CountPending reports how many rows currently sit in StatePending,
	// independent of any ClaimPending limit — the queue_depth metric's
	// source (docs/PLAN.md Task 31).
	CountPending(ctx context.Context) (int, error)
	// MarkSent transitions id to StateSent.
	MarkSent(ctx context.Context, id string) error
	// MarkAttemptFailed records a transient failure: increments Attempts
	// and LastError but leaves the row in StatePending so it is retried.
	MarkAttemptFailed(ctx context.Context, id, errMsg string) error
	// MarkDeadLetter transitions id to the terminal StateFailed — the
	// dead-letter path for permanently-failing sends (attempts exhausted
	// or a non-retryable classification).
	MarkDeadLetter(ctx context.Context, id, errMsg string) error
	// ScheduleRetry persists id's not-before time so a transient failure's
	// backoff (and Telegram's authoritative retry_after) survives a daemon
	// restart (docs/PLAN.md Task 112 / INT-04). ClaimPending must not return
	// a row whose NextAttemptAt is still in the future.
	ScheduleRetry(ctx context.Context, id string, notBefore time.Time) error
}

// OffsetStore durably records the Telegram getUpdates offset per bot so a
// daemon restart resumes exactly where it stopped — no re-delivery, no gap
// (docs/PLAN.md Task 112 / INT-04, step 2).
type OffsetStore interface {
	// GetOffset returns the last processed update id for botID, or 0 when the
	// bot has no recorded offset yet.
	GetOffset(ctx context.Context, botID string) (int64, error)
	// SetOffset advances botID's offset to updateID. It is monotonic: an
	// updateID not greater than the stored value is ignored.
	SetOffset(ctx context.Context, botID string, updateID int64) error
}

// PostgresStore is the Postgres-backed Store implementation, wrapping
// internal/db/migrations/00007_notifications.sql's `notifications`
// table.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore wraps an existing *sql.DB (opened with the pgx
// driver, matching internal/ledger/extops.NewStore's precedent).
func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) Enqueue(ctx context.Context, id, channel, target, class string, payload []byte) error {
	const insert = `
INSERT INTO notifications (id, channel, target, class, payload)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, insert, id, channel, target, class, payload); err != nil {
		return fmt.Errorf("notify: enqueue %s: %w", id, err)
	}
	return nil
}

func (s *PostgresStore) ClaimPending(ctx context.Context, limit int) ([]Notification, error) {
	const q = `
SELECT id, channel, target, class, payload, state, attempts, COALESCE(last_error, ''), created_at, updated_at
FROM notifications
WHERE state = 'pending' AND (next_attempt_at IS NULL OR next_attempt_at <= now())
ORDER BY created_at ASC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("notify: claim pending: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Notification
	for rows.Next() {
		var n Notification
		var state string
		if err := rows.Scan(&n.ID, &n.Channel, &n.Target, &n.Class, &n.Payload, &state, &n.Attempts, &n.LastError, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("notify: scan pending row: %w", err)
		}
		n.State = State(state)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notify: claim pending: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) CountPending(ctx context.Context) (int, error) {
	const q = `SELECT count(*) FROM notifications WHERE state = 'pending'`
	var count int
	if err := s.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return 0, fmt.Errorf("notify: count pending: %w", err)
	}
	return count, nil
}

func (s *PostgresStore) MarkSent(ctx context.Context, id string) error {
	const update = `UPDATE notifications SET state = 'sent', sent_at = now(), updated_at = now() WHERE id = $1`
	res, err := s.db.ExecContext(ctx, update, id)
	if err != nil {
		return fmt.Errorf("notify: mark sent %s: %w", id, err)
	}
	return mustAffectOne(res, id)
}

func (s *PostgresStore) MarkAttemptFailed(ctx context.Context, id, errMsg string) error {
	const update = `UPDATE notifications SET attempts = attempts + 1, last_error = $2, updated_at = now() WHERE id = $1`
	res, err := s.db.ExecContext(ctx, update, id, errMsg)
	if err != nil {
		return fmt.Errorf("notify: mark attempt failed %s: %w", id, err)
	}
	return mustAffectOne(res, id)
}

func (s *PostgresStore) MarkDeadLetter(ctx context.Context, id, errMsg string) error {
	const update = `UPDATE notifications SET state = 'failed', last_error = $2, updated_at = now() WHERE id = $1`
	res, err := s.db.ExecContext(ctx, update, id, "dead-letter: "+errMsg)
	if err != nil {
		return fmt.Errorf("notify: mark dead letter %s: %w", id, err)
	}
	return mustAffectOne(res, id)
}

// ErrNotificationNotFound is returned when id does not name an existing
// notifications row.
var ErrNotificationNotFound = errors.New("notify: notification not found")

func mustAffectOne(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("notify: rows affected for %s: %w", id, err)
	}
	if n == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

// ScheduleRetry persists id's not-before time (durable pacing, Task 112).
func (s *PostgresStore) ScheduleRetry(ctx context.Context, id string, notBefore time.Time) error {
	const update = `UPDATE notifications SET next_attempt_at = $2, updated_at = now() WHERE id = $1`
	res, err := s.db.ExecContext(ctx, update, id, notBefore)
	if err != nil {
		return fmt.Errorf("notify: schedule retry %s: %w", id, err)
	}
	return mustAffectOne(res, id)
}

// GetOffset returns botID's last processed getUpdates offset (0 if none).
func (s *PostgresStore) GetOffset(ctx context.Context, botID string) (int64, error) {
	const q = `SELECT last_update_id FROM telegram_offsets WHERE bot_id = $1`
	var id int64
	err := s.db.QueryRowContext(ctx, q, botID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("notify: get offset %s: %w", botID, err)
	}
	return id, nil
}

// SetOffset advances botID's offset monotonically.
func (s *PostgresStore) SetOffset(ctx context.Context, botID string, updateID int64) error {
	const q = `
INSERT INTO telegram_offsets (bot_id, last_update_id, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (bot_id) DO UPDATE
SET last_update_id = GREATEST(telegram_offsets.last_update_id, EXCLUDED.last_update_id),
    updated_at = now()`
	if _, err := s.db.ExecContext(ctx, q, botID, updateID); err != nil {
		return fmt.Errorf("notify: set offset %s: %w", botID, err)
	}
	return nil
}
