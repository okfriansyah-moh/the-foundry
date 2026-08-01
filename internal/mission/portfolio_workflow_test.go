package mission

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestPortfolioLoop_ContinuesAsNew proves the supervisor's bounded-history
// behavior: after MaxIterations ticks it continues-as-new (preserving its
// iteration counter) rather than growing Temporal history without bound, and it
// calls ReconcilePortfolio exactly once per tick.
func TestPortfolioLoop_ContinuesAsNew(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var calls int
	env.RegisterActivityWithOptions(
		func(_ context.Context, in PortfolioReconcileInput) (PortfolioReconcileResult, error) {
			calls++
			if in.PortfolioID != "pf1" {
				t.Errorf("reconcile got portfolio %q, want pf1", in.PortfolioID)
			}
			// No active missions: exercises the loop/CAN skeleton without
			// needing a child MissionLoop registered.
			return PortfolioReconcileResult{}, nil
		},
		activity.RegisterOptions{Name: ActivityPortfolioReconcile},
	)
	env.RegisterWorkflow(PortfolioLoop)

	env.ExecuteWorkflow(PortfolioLoop, PortfolioLoopInput{
		PortfolioID:   "pf1",
		MaxIterations: 1,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var canErr *workflow.ContinueAsNewError
	if err := env.GetWorkflowError(); !errors.As(err, &canErr) {
		t.Fatalf("want ContinueAsNewError after MaxIterations, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("ReconcilePortfolio called %d times, want 1", calls)
	}
}

// TestPortfolioWorkflowIDStable proves the supervisor's workflow ID is a pure
// function of the portfolio ID, so a restart racing a manual start collapses to
// one supervisor rather than two both racing the cap.
func TestPortfolioWorkflowIDStable(t *testing.T) {
	if a, b := PortfolioWorkflowID("pf1"), PortfolioWorkflowID("pf1"); a != b {
		t.Fatalf("workflow ID not stable: %q vs %q", a, b)
	}
	if PortfolioWorkflowID("pf1") == PortfolioWorkflowID("pf2") {
		t.Fatal("distinct portfolios must get distinct workflow IDs")
	}
}
