package mission

import (
	"errors"
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestValidateDeliverInput proves the Task 106 payload guard: an empty or
// malformed DeliverPlanInput is refused, a complete one is accepted.
func TestValidateDeliverInput(t *testing.T) {
	if err := validateDeliverInput(kernel.DeliverPlanInput{}); err == nil {
		t.Fatal("empty input must be refused")
	}
	if err := validateDeliverInput(kernel.DeliverPlanInput{PlanID: "p"}); err == nil {
		t.Fatal("missing plan file path must be refused")
	}
	if err := validateDeliverInput(kernel.DeliverPlanInput{PlanID: "p", PlanFilePath: "f"}); err == nil {
		t.Fatal("missing repo path must be refused")
	}
	if err := validateDeliverInput(kernel.DeliverPlanInput{PlanID: "p", PlanFilePath: "f", RepoPath: "r"}); err != nil {
		t.Fatalf("complete input must be accepted, got %v", err)
	}
}

func TestChildResultFailed(t *testing.T) {
	if !childResultFailed(kernel.DeliverPlanResult{Status: "FAILED"}) {
		t.Fatal("FAILED result must report failed")
	}
	if childResultFailed(kernel.DeliverPlanResult{Status: "SUCCEEDED"}) {
		t.Fatal("SUCCEEDED result must not report failed")
	}
}

// TestMissionLoop_ContinuesAsNew proves docs/PLAN.md Task 106's bounded-history
// behavior: after MaxIterationsBeforeContinue observe cycles, MissionLoop
// continues-as-new (preserving loop state) rather than growing history without
// bound.
func TestMissionLoop_ContinuesAsNew(t *testing.T) {
	// Below-target-but-progressing-then-flat samples keep the loop in the
	// Continue state across the two cycles before the continue-as-new bound.
	fx := newMissionFixture([]LedgerSample{belowSample(0), belowSample(1), belowSample(2)})

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerMissionActivities(env, fx.Activities)
	env.RegisterWorkflow(MissionLoop)

	env.ExecuteWorkflow(MissionLoop, MissionLoopInput{
		MissionID:                   "m1",
		Contract:                    testContract(),
		MaxIterationsBeforeContinue: 2,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	err := env.GetWorkflowError()
	var canErr *workflow.ContinueAsNewError
	if !errors.As(err, &canErr) {
		t.Fatalf("expected a ContinueAsNewError after the iteration bound, got %v", err)
	}
}
