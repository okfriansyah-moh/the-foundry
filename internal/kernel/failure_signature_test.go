package kernel

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeFailureSignatureStore records inserts keyed by row id so the idempotency
// assertion can count distinct rows.
type fakeFailureSignatureStore struct {
	mu   sync.Mutex
	rows map[string]int
}

func (f *fakeFailureSignatureStore) RecordFailureSignature(_ context.Context, id, _, _ string, _ int, _, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[string]int{}
	}
	f.rows[id]++
	return nil
}

// TestRecordFailureSignature_Idempotent proves docs/PLAN.md Task 123: a retried
// RecordFailureSignature for the same (workflow, task, attempt) writes exactly
// one row — the receipt short-circuits the retry, and the deterministic row id
// would collapse it at the DB layer even if it did not.
func TestRecordFailureSignature_Idempotent(t *testing.T) {
	store := &fakeFailureSignatureStore{}
	acts := &Activities{FailureSignatures: store, ReceiptStore: NewMemReceiptStore()}
	in := RecordFailureSignatureInput{
		WorkflowID:     "wf1",
		TaskID:         "task-1",
		Attempt:        2,
		Classification: "verification-failed",
		DetailDigest:   "digestX",
		OccurredAt:     time.Unix(1_700_000_000, 0).UTC(),
	}
	for i := 0; i < 3; i++ {
		if err := acts.RecordFailureSignature(context.Background(), in); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	if len(store.rows) != 1 {
		t.Fatalf("distinct rows = %d, want 1", len(store.rows))
	}

	// A nil store makes recording a no-op (supervision-off callers unaffected).
	nilActs := &Activities{ReceiptStore: NewMemReceiptStore()}
	if err := nilActs.RecordFailureSignature(context.Background(), in); err != nil {
		t.Fatalf("nil-store record must be a no-op, got %v", err)
	}
}

// TestFailureDetailDigest_Stable proves the digest is a stable, normalized
// fingerprint: the same task+classification+commands yields the same digest
// (so "the same failure N times" compares equal), and different inputs differ.
func TestFailureDetailDigest_Stable(t *testing.T) {
	a := FailureDetailDigest("task-1", "verification-failed", []string{"go test ./..."})
	b := FailureDetailDigest("task-1", "verification-failed", []string{"go test ./..."})
	if a != b {
		t.Fatalf("digest not stable: %q vs %q", a, b)
	}
	if a == FailureDetailDigest("task-2", "verification-failed", []string{"go test ./..."}) {
		t.Fatal("distinct tasks must yield distinct digests")
	}
	if a == FailureDetailDigest("task-1", "policy-violation", []string{"go test ./..."}) {
		t.Fatal("distinct classifications must yield distinct digests")
	}
}
