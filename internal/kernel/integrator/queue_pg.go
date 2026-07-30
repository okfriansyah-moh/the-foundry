package integrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PGQueue is the Postgres-backed integration queue and receipt store
// (docs/PLAN.md Task 108 / RTC-04), closing the "integration_queue /
// integration_receipts have no Go reader" gap the audit named. It serializes
// per-branch claims with FOR UPDATE SKIP LOCKED plus a per-branch transaction
// advisory lock, so two workers never process the same branch concurrently and
// a retry never double-pushes. The in-memory Queue stays for unit tests; this
// is the production backing store (internal/db/migrations/00020_integration_queue.sql).
type PGQueue struct {
	db *sql.DB
}

// NewPGQueue wraps an existing *sql.DB.
func NewPGQueue(db *sql.DB) *PGQueue { return &PGQueue{db: db} }

// Enqueue appends an item to the persistent per-branch queue.
func (q *PGQueue) Enqueue(ctx context.Context, item IntegrationItem) error {
	const stmt = `
INSERT INTO integration_queue
  (id, branch, group_id, manifest_digest, commits, expected_base, status, enqueued_at)
VALUES ($1,$2,$3,$4,string_to_array($5, ','),$6,'pending', now())`
	if _, err := q.db.ExecContext(ctx, stmt,
		item.ID, item.Branch, item.GroupID, item.ManifestDigest, strings.Join(item.Commits, ","), item.ExpectedBase,
	); err != nil {
		return fmt.Errorf("integrator: enqueue %s: %w", item.ID, err)
	}
	return nil
}

// Claim atomically claims the next pending item for a branch, marking it
// processing, or returns ok=false when none is pending. A per-branch advisory
// lock plus FOR UPDATE SKIP LOCKED guarantees exactly-once claiming across
// concurrent workers.
func (q *PGQueue) Claim(ctx context.Context, branch string) (IntegrationItem, bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return IntegrationItem{}, false, fmt.Errorf("integrator: begin claim tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, branch); err != nil {
		return IntegrationItem{}, false, fmt.Errorf("integrator: advisory lock %s: %w", branch, err)
	}

	const sel = `
SELECT id, branch, group_id, manifest_digest, array_to_string(commits, ','), expected_base, enqueued_at
FROM integration_queue
WHERE branch = $1 AND status = 'pending'
ORDER BY enqueued_at ASC
FOR UPDATE SKIP LOCKED
LIMIT 1`
	var it IntegrationItem
	var commitsCSV string
	err = tx.QueryRowContext(ctx, sel, branch).Scan(
		&it.ID, &it.Branch, &it.GroupID, &it.ManifestDigest, &commitsCSV, &it.ExpectedBase, &it.EnqueuedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return IntegrationItem{}, false, fmt.Errorf("integrator: commit empty claim: %w", err)
		}
		committed = true
		return IntegrationItem{}, false, nil
	}
	if err != nil {
		return IntegrationItem{}, false, fmt.Errorf("integrator: select claim %s: %w", branch, err)
	}
	if commitsCSV != "" {
		it.Commits = strings.Split(commitsCSV, ",")
	}

	if _, err := tx.ExecContext(ctx, `UPDATE integration_queue SET status='processing' WHERE id=$1`, it.ID); err != nil {
		return IntegrationItem{}, false, fmt.Errorf("integrator: mark processing %s: %w", it.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return IntegrationItem{}, false, fmt.Errorf("integrator: commit claim: %w", err)
	}
	committed = true
	return it, true, nil
}

// Complete marks a claimed item done.
func (q *PGQueue) Complete(ctx context.Context, id string) error {
	if _, err := q.db.ExecContext(ctx, `UPDATE integration_queue SET status='done', processed_at=now() WHERE id=$1`, id); err != nil {
		return fmt.Errorf("integrator: complete %s: %w", id, err)
	}
	return nil
}

// Requeue returns an item to pending with a fresh expected base (used after a
// drift-induced rebase, Task 59).
func (q *PGQueue) Requeue(ctx context.Context, id, newExpectedBase, reason string) error {
	if _, err := q.db.ExecContext(ctx,
		`UPDATE integration_queue SET status='pending', expected_base=$2, error_msg=$3 WHERE id=$1`,
		id, newExpectedBase, reason,
	); err != nil {
		return fmt.Errorf("integrator: requeue %s: %w", id, err)
	}
	return nil
}

// Fail marks a claimed item failed with a reason.
func (q *PGQueue) Fail(ctx context.Context, id, reason string) error {
	if _, err := q.db.ExecContext(ctx,
		`UPDATE integration_queue SET status='failed', processed_at=now(), error_msg=$2 WHERE id=$1`, id, reason,
	); err != nil {
		return fmt.Errorf("integrator: fail %s: %w", id, err)
	}
	return nil
}

// RecordReceipt persists a push receipt, satisfying the ReceiptStore interface
// so a PGQueue can back an Integrator directly.
func (q *PGQueue) RecordReceipt(ctx context.Context, r Receipt) error {
	id := r.GroupID + ":" + r.Branch + ":" + r.AfterSHA
	const stmt = `
INSERT INTO integration_receipts (id, branch, before_sha, after_sha, group_id, manifest_digest, issued_at)
VALUES ($1,$2,$3,$4,$5,$6, COALESCE($7, now()))
ON CONFLICT (id) DO NOTHING`
	var issuedAt any
	if !r.IssuedAt.IsZero() {
		issuedAt = r.IssuedAt
	}
	if _, err := q.db.ExecContext(ctx, stmt, id, r.Branch, r.BeforeSHA, r.AfterSHA, r.GroupID, r.ManifestDigest, issuedAt); err != nil {
		return fmt.Errorf("integrator: record receipt for %s: %w", r.GroupID, err)
	}
	return nil
}

// ReceiptForGroup returns a previously recorded receipt for a group+branch, so
// a retried integration short-circuits on its receipt instead of pushing again
// (idempotency).
func (q *PGQueue) ReceiptForGroup(ctx context.Context, groupID, branch string) (Receipt, bool, error) {
	const sel = `
SELECT branch, before_sha, after_sha, group_id, manifest_digest, issued_at
FROM integration_receipts
WHERE group_id = $1 AND branch = $2
ORDER BY issued_at DESC
LIMIT 1`
	var r Receipt
	err := q.db.QueryRowContext(ctx, sel, groupID, branch).Scan(
		&r.Branch, &r.BeforeSHA, &r.AfterSHA, &r.GroupID, &r.ManifestDigest, &r.IssuedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, fmt.Errorf("integrator: receipt for group %s: %w", groupID, err)
	}
	return r, true, nil
}
