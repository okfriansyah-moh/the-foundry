package kernel_test

import (
	"os"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

const scriptSuccess = `
patches:
  - path: out.txt
    content: "done\n"
claimed: "all good"
exit_code: 0
`

const scriptFailure = `
claimed: "all good anyway"
exit_notes: "lying summary"
exit_code: 1
`

// TestDeliverPlan_HelloWorld drives the workflow end to end (using
// go.temporal.io/sdk/testsuite, so no live Temporal server is needed) with
// a single task whose fake executor script succeeds. It must reach
// SUCCEEDED with one recorded evidence bundle and RUNNING+SUCCEEDED
// transitions durably appended.
func TestDeliverPlan_HelloWorld(t *testing.T) {
	fx := newFixture(t, scriptSuccess)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	var result kernel.DeliverPlanResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusSucceeded) {
		t.Fatalf("status = %q, want %q", result.Status, state.StatusSucceeded)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].Failed {
		t.Fatalf("tasks = %+v, want one non-failed task", result.Tasks)
	}

	workflowID := "default-test-workflow-id"
	transitions := fx.Transitions.All(workflowID)
	if len(transitions) != 2 {
		t.Fatalf("transitions = %d, want 2 (RUNNING then SUCCEEDED)", len(transitions))
	}
	if transitions[0].Status != state.StatusRunning {
		t.Fatalf("transitions[0].Status = %q, want RUNNING", transitions[0].Status)
	}
	if transitions[1].Status != state.StatusSucceeded {
		t.Fatalf("transitions[1].Status = %q, want SUCCEEDED", transitions[1].Status)
	}
	if transitions[1].Reason != "" {
		t.Fatalf("SUCCEEDED transition Reason = %q, want empty (state.Transition.Validate forbids it)", transitions[1].Reason)
	}
}

// TestDeliverPlan_FailingTask drives the workflow with a fake executor
// script that reports failure (a "lying summary" claim contradicted by a
// nonzero exit code) and asserts the honest-completion contract: the
// workflow ends FAILED with classification verification-failed, not
// SUCCEEDED, regardless of the executor's own claim.
func TestDeliverPlan_FailingTask(t *testing.T) {
	fx := newFixture(t, scriptFailure)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	var result kernel.DeliverPlanResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if result.Status != string(state.StatusFailed) {
		t.Fatalf("status = %q, want %q", result.Status, state.StatusFailed)
	}
	if result.ResultCode != "verification-failed" {
		t.Fatalf("result code = %q, want verification-failed", result.ResultCode)
	}
	if len(result.Tasks) != 1 || !result.Tasks[0].Failed {
		t.Fatalf("tasks = %+v, want one failed task", result.Tasks)
	}

	workflowID := "default-test-workflow-id"
	transitions := fx.Transitions.All(workflowID)
	if len(transitions) != 2 {
		t.Fatalf("transitions = %d, want 2 (RUNNING then FAILED)", len(transitions))
	}
	if transitions[1].Status != state.StatusFailed {
		t.Fatalf("transitions[1].Status = %q, want FAILED", transitions[1].Status)
	}
	if transitions[1].Reason != "verification-failed" {
		t.Fatalf("transitions[1].Reason = %q, want verification-failed", transitions[1].Reason)
	}
}

// TestDeliverPlan_TamperedPlan proves Constitution C7/C2 fidelity: a plan
// file edited after approval fails LoadApprovedPlan's digest check and the
// workflow never reaches ExecuteTask at all.
func TestDeliverPlan_TamperedPlan(t *testing.T) {
	fx := newFixture(t, scriptSuccess)

	tampered := fx.PlanFilePath
	orig, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatalf("read plan file: %v", err)
	}
	if err := os.WriteFile(tampered, append(orig, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatalf("tamper plan file: %v", err)
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("expected workflow error for a tampered plan file, got nil")
	}

	// The canonical lifecycle has no PENDING->FAILED edge (state-model.md
	// §1): even a plan that fails on its very first activity passes
	// through RUNNING first, so two transitions are expected here.
	transitions := fx.Transitions.All("default-test-workflow-id")
	if len(transitions) != 2 {
		t.Fatalf("transitions = %d, want 2 (RUNNING then FAILED admission-rejected)", len(transitions))
	}
	if transitions[1].Status != state.StatusFailed || transitions[1].Reason != "admission-rejected" {
		t.Fatalf("transitions[1] = %+v, want FAILED/admission-rejected", transitions[1])
	}
}
