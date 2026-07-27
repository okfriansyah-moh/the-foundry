package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// memExternalOpStore is an in-memory ExternalOpStore for pure-logic tests
// of WithExternalOp — the real, Postgres-backed, crash-injection proof of
// replay-safety lives in internal/ledger/extops/crash_injection_test.go
// (it exercises this exact WithExternalOp function against a real DB and
// a real killed process).
type memExternalOpStore struct {
	mu  sync.Mutex
	ops map[string]extops.Op
	seq int
}

func newMemExternalOpStore() *memExternalOpStore {
	return &memExternalOpStore{ops: make(map[string]extops.Op)}
}

func (m *memExternalOpStore) Reserve(_ context.Context, workflowID, kind, target, idempotencyKey string, request any) (extops.Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op, ok := m.ops[idempotencyKey]; ok {
		return op, nil
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return extops.Op{}, err
	}
	m.seq++
	op := extops.Op{
		ID:             extops.OpID(idempotencyKey),
		WorkflowID:     workflowID,
		Kind:           kind,
		Target:         target,
		IdempotencyKey: idempotencyKey,
		State:          extops.StateReserved,
		Request:        payload,
	}
	m.ops[idempotencyKey] = op
	return op, nil
}

func (m *memExternalOpStore) MarkExecuted(_ context.Context, id extops.OpID, receipt any) (extops.Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	op, ok := m.ops[string(id)]
	if !ok {
		return extops.Op{}, extops.ErrOpNotFound
	}
	if op.State != extops.StateReserved {
		return extops.Op{}, extops.ErrNotReserved
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return extops.Op{}, err
	}
	op.State = extops.StateExecuted
	op.Receipt = payload
	m.ops[string(id)] = op
	return op, nil
}

type pushReceipt struct {
	SHA string `json:"sha"`
}

func TestWithExternalOp_RunsFnExactlyOnce(t *testing.T) {
	store := newMemExternalOpStore()
	ctx := context.Background()
	calls := 0

	fn := func(context.Context) (pushReceipt, error) {
		calls++
		return pushReceipt{SHA: "abc123"}, nil
	}

	first, err := WithExternalOp(ctx, store, "wf-1", "scm.push", "org/repo#main", "key-1", map[string]string{"sha": "abc123"}, fn)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.SHA != "abc123" {
		t.Fatalf("first receipt = %+v", first)
	}
	if calls != 1 {
		t.Fatalf("calls after first invocation = %d, want 1", calls)
	}

	second, err := WithExternalOp(ctx, store, "wf-1", "scm.push", "org/repo#main", "key-1", map[string]string{"sha": "abc123"}, fn)
	if err != nil {
		t.Fatalf("second call (replay): %v", err)
	}
	if second != first {
		t.Fatalf("second call returned %+v, want the recorded receipt %+v", second, first)
	}
	if calls != 1 {
		t.Fatalf("calls after replay = %d, want 1 — fn was re-invoked on a replay with the same idempotency key", calls)
	}
}

func TestWithExternalOp_FailedFnLeavesOpReservedForRetry(t *testing.T) {
	store := newMemExternalOpStore()
	ctx := context.Background()
	calls := 0
	boom := errors.New("provider unreachable")

	fn := func(context.Context) (pushReceipt, error) {
		calls++
		if calls == 1 {
			return pushReceipt{}, boom
		}
		return pushReceipt{SHA: "abc123"}, nil
	}

	_, err := WithExternalOp(ctx, store, "wf-1", "scm.push", "org/repo#main", "key-2", map[string]string{"sha": "abc123"}, fn)
	if !errors.Is(err, boom) {
		t.Fatalf("first call error = %v, want %v", err, boom)
	}

	result, err := WithExternalOp(ctx, store, "wf-1", "scm.push", "org/repo#main", "key-2", map[string]string{"sha": "abc123"}, fn)
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if result.SHA != "abc123" {
		t.Fatalf("retry result = %+v", result)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (fn must retry after a failed attempt, since no side effect was confirmed)", calls)
	}
}
