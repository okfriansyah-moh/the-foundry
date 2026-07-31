package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// docs/PLAN.md Task 120 (COST-02): close the reserve→incur half of the cost
// ledger. RecordCost is called on task completion with the reservation entry id
// and the executor's real reported usage; it prices the usage (provider dollar
// figure, else the rate table, else an explicit "unknown"), incurs it against
// the reservation, and wires the cost_per_task metric to the INCURRED amount,
// not the reservation estimate.

// RecordCostInput carries the reservation to close and the real usage observed.
type RecordCostInput struct {
	WorkflowID string
	TaskID     string
	Attempt    int
	// EntryID is the reservation entry (from ReserveBudget) to incur against.
	// Empty means there is nothing to incur (e.g. an unmetered attended run).
	EntryID      string
	ExecutorName string
	Usage        executor.Usage
}

// RecordCostOutput reports the incurred amount and whether it was priced from a
// real figure or recorded as unknown (no rate / no signal).
type RecordCostOutput struct {
	IncurredUSD float64
	Unknown     bool
}

// RecordCost incurs a task's real cost against its reservation, idempotency-keyed
// like every other kernel activity. A model with no rate and no provider dollar
// figure records unknown rather than a fabricated default (Task 120).
func (a *Activities) RecordCost(ctx context.Context, in RecordCostInput) (RecordCostOutput, error) {
	key := IdempotencyKey{in.WorkflowID, in.TaskID, "RecordCost", in.Attempt}.String()
	return withReceipt(ctx, a.ReceiptStore, key, func() (RecordCostOutput, error) {
		if in.EntryID == "" {
			return RecordCostOutput{}, nil
		}
		usd, priceErr := a.ModelRates.PriceUsage(
			in.Usage.Model, in.Usage.InputTokens, in.Usage.OutputTokens,
			in.Usage.CachedTokens, in.Usage.ProviderReportedUSD)
		var unknownErr cost.PriceUnknownError
		if errors.As(priceErr, &unknownErr) {
			// Refuse to fabricate a figure — record unknown (incur 0, marked).
			return RecordCostOutput{Unknown: true}, nil
		}
		if priceErr != nil {
			return RecordCostOutput{}, fmt.Errorf("kernel: price usage %s/%s: %w", in.WorkflowID, in.TaskID, priceErr)
		}
		if a.CostStore == nil {
			return RecordCostOutput{IncurredUSD: usd}, nil
		}
		if _, err := a.CostStore.Incur(ctx, in.EntryID, usd); err != nil {
			return RecordCostOutput{}, fmt.Errorf("kernel: incur cost %s/%s: %w", in.WorkflowID, in.TaskID, err)
		}
		// Task 120: the cost_per_task metric reports the INCURRED amount now,
		// not the reservation estimate.
		observe.ObserveCostPerTask(in.ExecutorName, usd)
		return RecordCostOutput{IncurredUSD: usd}, nil
	})
}
