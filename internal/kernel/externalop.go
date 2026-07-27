package kernel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// ExternalOpStore is the subset of extops.Store's behavior WithExternalOp
// depends on (Constitution C4/C9: every external side effect the kernel
// makes is reserved before it is attempted and recorded once it lands —
// see internal/ledger/extops/doc.go for how this relates to
// internal/kernel/idempotency.go's separate, per-activity ReceiptStore).
type ExternalOpStore interface {
	Reserve(ctx context.Context, workflowID, kind, target, idempotencyKey string, request any) (extops.Op, error)
	MarkExecuted(ctx context.Context, id extops.OpID, receipt any) (extops.Op, error)
}

// WithExternalOp runs fn at most once per idempotencyKey against a real
// external system: it reserves the operation, and if a prior attempt
// already recorded it executed (or reconciled) — including one that
// crashed after committing that fact but before this call could observe
// it — returns the previously recorded receipt without invoking fn again.
// This is what makes an activity that pushes to GitHub, charges Stripe,
// etc. safe to retry after a Temporal-level activity retry or a real
// worker crash-restart.
//
// If the operation is reserved but not yet executed (fn was never
// confirmed to have run — e.g. a crash between Reserve and the side
// effect actually happening, or between the side effect happening and
// MarkExecuted's write landing), fn runs again. Closing that specific
// ambiguous window requires either a provider-side idempotency key (the
// concrete case Task 27's GitHub push CAS handles) or reconciliation
// against the provider's observed state (internal/ledger.Reconciler,
// stubbed by this task, given its first real prober in Task 27) — fn
// itself must be safe to invoke more than once for that window, exactly
// as N9.2 requires ("invoke with an idempotency key where supported").
func WithExternalOp[T any](
	ctx context.Context,
	store ExternalOpStore,
	workflowID, kind, target, idempotencyKey string,
	request any,
	fn func(ctx context.Context) (T, error),
) (T, error) {
	var zero T

	op, err := store.Reserve(ctx, workflowID, kind, target, idempotencyKey, request)
	if err != nil {
		return zero, fmt.Errorf("kernel: reserve external op %s: %w", idempotencyKey, err)
	}

	if op.State == extops.StateExecuted || op.State == extops.StateReconciled {
		observe.IncDuplicateSideEffectPrevented()
		var receipt T
		if err := json.Unmarshal(op.Receipt, &receipt); err != nil {
			return zero, fmt.Errorf("kernel: decode receipt for external op %s: %w", idempotencyKey, err)
		}
		return receipt, nil
	}

	result, err := fn(ctx)
	if err != nil {
		// fn's own side effect is not confirmed to have happened — leave
		// the operation reserved so a future call retries it, matching
		// N9.2 rule 5 ("never assume a timeout/failure means no side
		// effect" cuts both ways: it also never assumes one *did* happen).
		return zero, err
	}

	if _, err := store.MarkExecuted(ctx, op.ID, result); err != nil {
		return zero, fmt.Errorf("kernel: mark external op %s executed: %w", idempotencyKey, err)
	}
	return result, nil
}
