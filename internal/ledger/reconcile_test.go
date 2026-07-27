package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// fakeReconcileStore is an in-memory ReconcileStore for pure-logic tests
// of Reconciler.RunOnce — no Postgres needed, since the store's own
// real-Postgres behavior is proven independently in
// internal/ledger/extops/store_test.go.
type fakeReconcileStore struct {
	executed   []extops.Op
	reconciled map[extops.OpID]json.RawMessage
}

func (f *fakeReconcileStore) ListByState(_ context.Context, state extops.State, limit int) ([]extops.Op, error) {
	if state != extops.StateExecuted {
		return nil, nil
	}
	if limit < len(f.executed) {
		return f.executed[:limit], nil
	}
	return f.executed, nil
}

func (f *fakeReconcileStore) Reconcile(_ context.Context, id extops.OpID, observed json.RawMessage) (bool, error) {
	for _, op := range f.executed {
		if op.ID != id {
			continue
		}
		if f.reconciled == nil {
			f.reconciled = make(map[extops.OpID]json.RawMessage)
		}
		f.reconciled[id] = observed
		return string(op.Receipt) != string(observed), nil
	}
	return false, errors.New("op not found")
}

func TestReconciler_RunOnce_SkipsKindWithoutRegisteredProber(t *testing.T) {
	store := &fakeReconcileStore{executed: []extops.Op{
		{ID: "op-1", Kind: "billing.charge", Receipt: json.RawMessage(`{"amount":100}`)},
	}}
	r := NewReconciler(store)
	// No prober registered for "billing.charge" — must be a silent no-op,
	// not an error (docs/PLAN.md Task 26 Step 5).
	if err := r.RunOnce(context.Background(), 10); err != nil {
		t.Fatalf("RunOnce with no registered prober returned an error: %v", err)
	}
	if len(store.reconciled) != 0 {
		t.Fatalf("op with no registered prober was reconciled anyway: %v", store.reconciled)
	}
}

type fakeProber struct {
	observed json.RawMessage
	err      error
}

func (p fakeProber) Observe(_ context.Context, _ extops.Op) (json.RawMessage, error) {
	return p.observed, p.err
}

func TestReconciler_RunOnce_ReconcilesMatchingKind(t *testing.T) {
	store := &fakeReconcileStore{executed: []extops.Op{
		{ID: "op-1", Kind: "scm.push", Receipt: json.RawMessage(`{"sha":"abc"}`)},
	}}
	r := NewReconciler(store)
	r.Register("scm.push", fakeProber{observed: json.RawMessage(`{"sha":"abc"}`)})

	before := testutil.ToFloat64(observe.ExternalOperationDivergence)
	if err := r.RunOnce(context.Background(), 10); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, ok := store.reconciled["op-1"]; !ok {
		t.Fatal("op-1 was not reconciled despite a registered prober")
	}
	if got := testutil.ToFloat64(observe.ExternalOperationDivergence); got != before {
		t.Fatalf("divergence metric = %v, want unchanged %v (observation matched receipt)", got, before)
	}
}

func TestReconciler_RunOnce_RecordsDivergence(t *testing.T) {
	store := &fakeReconcileStore{executed: []extops.Op{
		{ID: "op-2", Kind: "scm.push", Receipt: json.RawMessage(`{"sha":"abc"}`)},
	}}
	r := NewReconciler(store)
	r.Register("scm.push", fakeProber{observed: json.RawMessage(`{"sha":"different"}`)})

	before := testutil.ToFloat64(observe.ExternalOperationDivergence)
	if err := r.RunOnce(context.Background(), 10); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := testutil.ToFloat64(observe.ExternalOperationDivergence); got != before+1 {
		t.Fatalf("divergence metric = %v, want %v", got, before+1)
	}
}
