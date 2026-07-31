package mission

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

// stubBudgets is a minimal BudgetStore for CheckBudget's fail-closed test.
type stubBudgets struct {
	monthlyErr error
	monthly    cost.Budget
	experiment cost.Budget
	experErr   error
}

func (s stubBudgets) GetBudget(_ context.Context, _ cost.Scope, _ string, kind cost.Kind, _ string) (cost.Budget, error) {
	if kind == cost.KindMissionMonthly {
		return s.monthly, s.monthlyErr
	}
	return s.experiment, s.experErr
}

// TestCheckBudget_NoEnvelopeFailsClosed proves Task 119 (COST-01): a mission
// with no provisioned monthly envelope halts (exhausted + no-envelope), rather
// than running unmetered as the previous empty case body allowed.
func TestCheckBudget_NoEnvelopeFailsClosed(t *testing.T) {
	a := &Activities{Budgets: stubBudgets{monthlyErr: cost.ErrBudgetNotFound, experErr: cost.ErrBudgetNotFound}}
	sig, err := a.CheckBudget(context.Background(), "m1")
	if err != nil {
		t.Fatalf("CheckBudget: %v", err)
	}
	if !sig.MonthlyBudgetExhausted || !sig.NoBudgetEnvelope {
		t.Fatalf("no envelope must fail closed: %+v", sig)
	}
}

// TestCheckBudget_HealthyEnvelopeContinues confirms a provisioned, unexhausted
// envelope does not halt the mission.
func TestCheckBudget_HealthyEnvelopeContinues(t *testing.T) {
	a := &Activities{Budgets: stubBudgets{
		monthly:    cost.Budget{CeilingUSD: 100, IncurredUSD: 10},
		experiment: cost.Budget{CeilingUSD: 100, IncurredUSD: 10},
	}}
	sig, err := a.CheckBudget(context.Background(), "m1")
	if err != nil {
		t.Fatalf("CheckBudget: %v", err)
	}
	if sig.MonthlyBudgetExhausted || sig.NoBudgetEnvelope || sig.TotalBudgetExhausted {
		t.Fatalf("healthy envelope must continue: %+v", sig)
	}
}
