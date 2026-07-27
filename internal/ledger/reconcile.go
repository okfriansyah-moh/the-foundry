package ledger

// reconcile.go is the reconciler that closes the loop N9.2 requires: once
// an external operation is recorded executed, its expected outcome must
// eventually be checked against what the provider actually observes.
//
// This is a skeleton (docs/PLAN.md Task 26/FND-07): it only dispatches to
// probers registered for a given operation kind. Task 27 registers the
// first real prober (a git-ref prober for scm.push); every other kind
// remains a deliberate no-op here, not an error — an unprobed kind simply
// cannot diverge from this reconciler's point of view yet.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// Prober observes the actual, current state of one external operation's
// target (e.g. "what SHA does this ref point at right now") so it can be
// compared against the receipt recorded when the operation was marked
// executed.
type Prober interface {
	Observe(ctx context.Context, op extops.Op) (observed json.RawMessage, err error)
}

// ReconcileStore is the subset of *extops.Store the Reconciler needs.
type ReconcileStore interface {
	ListByState(ctx context.Context, state extops.State, limit int) ([]extops.Op, error)
	Reconcile(ctx context.Context, id extops.OpID, observed json.RawMessage) (diverged bool, err error)
}

// Reconciler compares expected vs. observed state for executed
// operations whose kind has a registered Prober.
type Reconciler struct {
	Store   ReconcileStore
	Probers map[string]Prober
}

// NewReconciler builds a Reconciler with an empty prober registry —
// callers register probers via Register before RunOnce is useful.
func NewReconciler(store ReconcileStore) *Reconciler {
	return &Reconciler{Store: store, Probers: make(map[string]Prober)}
}

// Register wires a Prober for one operation kind (e.g. "scm.push").
func (r *Reconciler) Register(kind string, p Prober) {
	r.Probers[kind] = p
}

// RunOnce reconciles up to limit executed operations. Kinds without a
// registered prober are skipped silently — this is the stub's explicit
// scope boundary, not a bug: a real, general-purpose scan/schedule loop
// is future work once more than one prober exists.
func (r *Reconciler) RunOnce(ctx context.Context, limit int) error {
	ops, err := r.Store.ListByState(ctx, extops.StateExecuted, limit)
	if err != nil {
		return fmt.Errorf("ledger: reconcile: list executed operations: %w", err)
	}

	for _, op := range ops {
		prober, ok := r.Probers[op.Kind]
		if !ok {
			continue
		}

		observed, err := prober.Observe(ctx, op)
		if err != nil {
			return fmt.Errorf("ledger: reconcile: observe %s (%s): %w", op.ID, op.Kind, err)
		}

		diverged, err := r.Store.Reconcile(ctx, op.ID, observed)
		if err != nil {
			return fmt.Errorf("ledger: reconcile: %s: %w", op.ID, err)
		}
		if diverged {
			observe.IncExternalOperationDivergence()
		}
	}

	return nil
}
