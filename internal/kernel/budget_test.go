package kernel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

func TestMemBudgetStore_UnmeteredWithoutCeiling(t *testing.T) {
	s := kernel.NewMemBudgetStore()
	_, err := s.Reserve(context.Background(), cost.ScopeWorkflow, "wf1", cost.KindMissionMonthly, "2026-07", 1.00, "openai", "v1", nil)
	if !errors.Is(err, cost.ErrBudgetNotFound) {
		t.Fatalf("error = %v, want ErrBudgetNotFound", err)
	}
}

func TestMemBudgetStore_ExhaustsAtCeiling(t *testing.T) {
	s := kernel.NewMemBudgetStore()
	s.SetCeiling(cost.ScopeWorkflow, "wf1", cost.KindMissionMonthly, "2026-07", 1.00)

	if _, err := s.Reserve(context.Background(), cost.ScopeWorkflow, "wf1", cost.KindMissionMonthly, "2026-07", 0.60, "openai", "v1", nil); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	_, err := s.Reserve(context.Background(), cost.ScopeWorkflow, "wf1", cost.KindMissionMonthly, "2026-07", 0.60, "openai", "v1", nil)
	if !errors.Is(err, cost.ErrBudgetExhausted) {
		t.Fatalf("second reserve error = %v, want ErrBudgetExhausted", err)
	}
}

func TestMemBudgetStore_RecordShadowNeverExhausts(t *testing.T) {
	s := kernel.NewMemBudgetStore()
	entry, err := s.RecordShadow(context.Background(), cost.ScopeWorkflow, "wf1", 999.00, "claudecode", "v1", nil)
	if err != nil {
		t.Fatalf("record shadow: %v", err)
	}
	if entry.State != cost.StateShadow {
		t.Fatalf("state = %q, want shadow", entry.State)
	}
}
