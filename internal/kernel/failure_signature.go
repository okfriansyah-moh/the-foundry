package kernel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// FailureSignatureStore persists one normalized failure signature per failed
// task attempt (docs/PLAN.md Task 123). It is the durable history the liveness
// supervisor's PoisonedTask/InfiniteRetry conditions classify against —
// internal/recovery reads the same table but never imports this package
// (Constitution: recovery never imports kernel), so the write side lives here
// and the read side lives in internal/recovery/postgres.go.
type FailureSignatureStore interface {
	// RecordFailureSignature inserts a signature idempotently: a repeated
	// (workflow_id, task_id, attempt) addresses the same row via ON CONFLICT
	// DO NOTHING, so a retry or a commit-then-crash never inflates the history.
	RecordFailureSignature(ctx context.Context, id, workflowID, taskID string, attempt int, classification, detailDigest string, occurredAt time.Time) error
}

// PGFailureSignatureStore is the Postgres-backed FailureSignatureStore
// (internal/db/migrations/00030_failure_signatures.sql).
type PGFailureSignatureStore struct {
	db *sql.DB
}

// NewPGFailureSignatureStore wraps an existing *sql.DB.
func NewPGFailureSignatureStore(db *sql.DB) *PGFailureSignatureStore {
	return &PGFailureSignatureStore{db: db}
}

// RecordFailureSignature implements FailureSignatureStore.
func (s *PGFailureSignatureStore) RecordFailureSignature(ctx context.Context, id, workflowID, taskID string, attempt int, classification, detailDigest string, occurredAt time.Time) error {
	const q = `
INSERT INTO task_failure_signatures (id, workflow_id, task_id, attempt, classification, detail_digest, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workflow_id, task_id, attempt) DO NOTHING`
	if _, err := s.db.ExecContext(ctx, q, id, workflowID, taskID, attempt, classification, detailDigest, occurredAt.UTC()); err != nil {
		return fmt.Errorf("kernel: record failure signature for %s/%s: %w", workflowID, taskID, err)
	}
	return nil
}

// FailureDetailDigest normalizes a task failure into a stable fingerprint:
// the task's identity plus its declared validation commands, with no
// timestamps, paths or run-specific text, so "the same failure N times" is a
// stable comparison rather than a string match on a volatile message
// (docs/PLAN.md Task 123). It is deterministic, safe to call from workflow code.
func FailureDetailDigest(taskID, classification string, validationCommands []string) string {
	sum := sha256.Sum256([]byte(taskID + "|" + classification + "|" + strings.Join(validationCommands, "\n")))
	return hex.EncodeToString(sum[:])[:32]
}

// RecordFailureSignatureInput is the RecordFailureSignature activity's input.
type RecordFailureSignatureInput struct {
	WorkflowID     string
	TaskID         string
	Attempt        int
	Classification string
	DetailDigest   string
	// OccurredAt is passed from workflow.Now(ctx) so a replayed record samples
	// the same instant.
	OccurredAt time.Time
}

// RecordFailureSignature implements ActivityRecordFailureSignature. It runs
// through the kernel's receipt wrapper AND writes under the table's
// (workflow_id, task_id, attempt) unique constraint, so neither a Temporal
// retry nor a commit-then-crash produces a duplicate signature (Constitution
// C9). A recording error never fails the plan — the caller ignores it, since a
// missing signature only weakens supervision, it does not corrupt state.
func (a *Activities) RecordFailureSignature(ctx context.Context, in RecordFailureSignatureInput) error {
	if a.FailureSignatures == nil {
		return nil
	}
	rowID := failureSignatureRowID(in.WorkflowID, in.TaskID, in.Attempt)
	key := IdempotencyKey{WorkflowID: in.WorkflowID, TaskID: in.TaskID, Activity: ActivityRecordFailureSignature, Attempt: in.Attempt}.String()
	_, err := withReceipt(ctx, a.receiptStore(), key, func() (struct{}, error) {
		if err := a.FailureSignatures.RecordFailureSignature(ctx, rowID, in.WorkflowID, in.TaskID, in.Attempt, in.Classification, in.DetailDigest, in.OccurredAt); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

func failureSignatureRowID(workflowID, taskID string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("failsig|%s|%s|%d", workflowID, taskID, attempt)))
	return "failsig-" + hex.EncodeToString(sum[:])[:24]
}

// receiptStore returns the configured receipt store, defaulting to an in-memory
// one so an Activities that was not wired with one still exercises the wrapper.
func (a *Activities) receiptStore() ReceiptStore {
	if a.ReceiptStore == nil {
		a.ReceiptStore = NewMemReceiptStore()
	}
	return a.ReceiptStore
}
