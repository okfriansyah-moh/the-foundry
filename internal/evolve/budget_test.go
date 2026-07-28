package evolve

import (
	"testing"
	"time"
)

func TestBudgetWindow_BreachesAndFreeze(t *testing.T) {
	limits := ChangeBudgetLimits{MaxPromotions: 1, MaxFilesChanged: 10, MaxRoutingWeightDelta: 0.5, MaxCostDeltaUSD: 50, MaxQualityRegression: 0.1, MaxRollbackDepth: 2}
	cases := []struct {
		name   string
		window BudgetWindow
		want   FreezeCondition
	}{
		{name: "budget", window: BudgetWindow{Promotions: 2}, want: FreezeBudgetExceeded},
		{name: "quality", window: BudgetWindow{QualityDelta: -0.2}, want: FreezeQualityRegression},
		{name: "cost", window: BudgetWindow{CostDeltaUSD: 100}, want: FreezeCostSpike},
		{name: "security", window: BudgetWindow{SecurityClassChanged: true}, want: FreezeSecurityClassChange},
		{name: "rollback", window: BudgetWindow{RollbackChainDepth: 3}, want: FreezeRollbackChainDepth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			breaches := tc.window.Breaches(limits)
			found := false
			for _, breach := range breaches {
				if breach == tc.want {
					found = true
					Freeze(breach)
					break
				}
			}
			if !found {
				t.Fatalf("breaches=%v want %s", breaches, tc.want)
			}
			if !IsFrozen() || FreezeReason() != tc.want {
				t.Fatalf("frozen=%v reason=%s", IsFrozen(), FreezeReason())
			}
			Unfreeze()
		})
	}
}

func TestBudgetWindow_CheckpointIntervalBreaches(t *testing.T) {
	limits := ChangeBudgetLimits{
		MaxHumanCheckpointInterval: 7 * 24 * time.Hour,
	}
	w := BudgetWindow{TimeSinceHumanCheckpoint: 8 * 24 * time.Hour}
	breaches := w.Breaches(limits)
	if len(breaches) == 0 {
		t.Fatal("expected FreezeBudgetExceeded for stale human checkpoint, got none")
	}
	found := false
	for _, b := range breaches {
		if b == FreezeBudgetExceeded {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected FreezeBudgetExceeded, got %v", breaches)
	}
}

func TestDefaultChangeBudgetLimits_Placeholder(t *testing.T) {
	limits := DefaultChangeBudgetLimits()
	if !limits.Placeholder {
		t.Fatal("default limits must be flagged placeholder: true until Blocker B7 is resolved")
	}
	if limits.MaxPromotions <= 0 {
		t.Fatal("default limits must have a positive MaxPromotions conservative value")
	}
}
