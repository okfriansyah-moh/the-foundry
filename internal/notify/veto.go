package notify

import (
	"context"
	"fmt"
	"time"
)

// VetoExecutor is the interface the CommandRouter calls when an operator
// issues /rollback <promo-id> <nonce>. The kernel owns the actual deploy
// rollback and plan revert; this interface is the seam between the notify
// layer (which handles the command/nonce lifecycle) and the kernel layer
// (which executes the rollback side effect). Constitution C4: only kernel
// performs side effects.
type VetoExecutor interface {
	// ExecuteVeto rolls back the promotion identified by promoID to its
	// RollbackRef, marks the promotions row vetoed, and records a
	// learning-evidence row. Returns the new deploy ref after rollback.
	ExecuteVeto(ctx context.Context, promoID string) (rollbackRef string, err error)
}

// VetoRecord extended — add Vetoed/VetoedAt setters (stored separately
// in DB; VetoRecord in digest.go is the in-memory projection).

// VetoCommand is parsed from the Telegram command "/rollback <promo-id> <nonce>".
type VetoCommand struct {
	PromotionID string
	Nonce       string
}

// handleRollback implements the /rollback <promo-id> <nonce> command.
// It validates the nonce, calls VetoExecutor.ExecuteVeto, then marks the
// promotion vetoed and evaluates the freeze condition.
func (r *CommandRouter) handleRollback(ctx context.Context, chatID string, args []string) string {
	if r.Veto == nil {
		return "rollback not configured"
	}
	if len(args) != 2 {
		return "usage: /rollback <promo-id> <nonce>"
	}
	promoID, nonce := args[0], args[1]

	// Consume nonce (bound to this promo-id as the "workflow" identifier).
	if err := r.Nonces.Consume(nonce, chatID, promoID); err != nil {
		return err.Error()
	}

	rollbackRef, err := r.Veto.ExecuteVeto(ctx, promoID)
	if err != nil {
		return fmt.Sprintf("veto failed: %v", err)
	}
	return fmt.Sprintf("rollback complete: promotion %s rolled back to %s", promoID, rollbackRef)
}

// NoopVetoExecutor is a zero-behavior VetoExecutor for tests and CLI-only
// deployments where no kernel is wired.
type NoopVetoExecutor struct{}

// ExecuteVeto logs and returns a no-op rollback ref.
func (NoopVetoExecutor) ExecuteVeto(_ context.Context, promoID string) (string, error) {
	return "noop-rollback-" + promoID, nil
}

// VetoRecord is already defined in digest.go; this file adds the LearningEvidence type
// used when recording why the veto occurred.
type LearningEvidence struct {
	PromotionID string
	VetoedAt    time.Time
	Reason      string
	RollbackRef string
}
