package kernel_test

import (
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
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
		ExecutorAllowlist: []string{"fake"},
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
		ExecutorAllowlist: []string{"fake"},
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

// TestDeliverPlan_LyingExecutorFailingValidation proves Constitution C10's
// honest-completion contract at the one place it matters end to end
// (docs/PLAN.md Task 99 / SKP-11R): the fake executor here claims success
// (scriptSuccess: "claimed: all good", exit_code: 0) exactly like
// TestDeliverPlan_HelloWorld, but this fixture's validation command is a
// real, allowlisted command that genuinely fails ("go" with an unknown
// subcommand). ValidateTask must classify from the real command's exit
// code, never from the executor's claim — the workflow must end FAILED
// with verify's real ClassificationVerificationFailed, not SUCCEEDED.
func TestDeliverPlan_LyingExecutorFailingValidation(t *testing.T) {
	fx := newFixtureWithValidation(t, scriptSuccess, "go this-subcommand-does-not-exist")

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
		ExecutorAllowlist: []string{"fake"},
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
		t.Fatalf("status = %q, want %q (executor's own claim of success must not decide this)", result.Status, state.StatusFailed)
	}
	if result.ResultCode != string(verify.ClassificationVerificationFailed) {
		t.Fatalf("result code = %q, want %q", result.ResultCode, verify.ClassificationVerificationFailed)
	}
	if len(result.Tasks) != 1 || !result.Tasks[0].Failed {
		t.Fatalf("tasks = %+v, want one failed task", result.Tasks)
	}

	transitions := fx.Transitions.All("default-test-workflow-id")
	if len(transitions) != 2 || transitions[1].Status != state.StatusFailed {
		t.Fatalf("transitions = %+v, want 2 (RUNNING then FAILED)", transitions)
	}
	if transitions[1].Reason != state.Reason(verify.ClassificationVerificationFailed) {
		t.Fatalf("transitions[1].Reason = %q, want %q", transitions[1].Reason, verify.ClassificationVerificationFailed)
	}
}

// TestDeliverPlan_ValidationClassificationPassesThrough proves the second,
// separate bug docs/PLAN.md Task 99 names: runTask must forward
// ValidateTask's real classification, not a literal "verification-failed"
// hardcoded regardless of what actually failed. This fixture's validation
// command ("curl", not on the fixture's own allowlist which only permits
// "go") produces internal/verify's distinct ClassificationPolicyViolation
// — a value the old hardcoded line could never have produced, so a
// passing assertion here is proof the real classification survives to
// DeliverPlanResult.ResultCode, not proof-by-coincidence.
func TestDeliverPlan_ValidationClassificationPassesThrough(t *testing.T) {
	fx := newFixtureWithValidation(t, scriptSuccess, "curl https://example.invalid")

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
		ExecutorAllowlist: []string{"fake"},
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
	if result.ResultCode != string(verify.ClassificationPolicyViolation) {
		t.Fatalf("result code = %q, want %q (not hardcoded verification-failed)", result.ResultCode, verify.ClassificationPolicyViolation)
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
		ExecutorAllowlist: []string{"fake"},
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

// TestDeliverPlan_BudgetExhausted_WaitsThenResumes proves Constitution
// C19's Acceptance bar (docs/PLAN.md Task 29): an exhausted budget
// envelope pauses the workflow to WAITING/budget rather than failing the
// plan, and it resumes — completing the same task, not restarting the
// plan — once the envelope is raised and SignalBudgetRaised is sent (the
// same signal `foundry budget raise` sends against a real workflow).
func TestDeliverPlan_BudgetExhausted_WaitsThenResumes(t *testing.T) {
	fx := newFixture(t, scriptSuccess)

	const workflowScopeID = "default-test-workflow-id" // testsuite's default workflow ID
	// period must match what the real ReserveBudget activity computes
	// (internal/kernel/budget.go's currentPeriod, unexported so this
	// external test package mirrors its calendar-month format directly).
	period := time.Now().Format("2006-01")

	// Ceiling below the fixture's 0.10 default estimate: the very first
	// ReserveBudget call for this workflow's one task must be exhausted.
	fx.BudgetStore.SetCeiling(cost.ScopeWorkflow, workflowScopeID, cost.KindMissionMonthly, period, 0.01)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerActivities(env, fx.Activities)
	env.RegisterWorkflow(kernel.DeliverPlan)

	// Simulate `foundry budget raise`: raise the ceiling, then signal the
	// workflow to resume — after some virtual-clock delay, so this
	// genuinely exercises the WAITING pause rather than racing past it.
	env.RegisterDelayedCallback(func() {
		fx.BudgetStore.SetCeiling(cost.ScopeWorkflow, workflowScopeID, cost.KindMissionMonthly, period, 10.00)
		env.SignalWorkflow(kernel.SignalBudgetRaised, nil)
	}, time.Minute)

	env.ExecuteWorkflow(kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
		ExecutorAllowlist: []string{"fake"},
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
		t.Fatalf("status = %q, want %q (must resume and finish the same task, not fail)", result.Status, state.StatusSucceeded)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].Failed {
		t.Fatalf("tasks = %+v, want one non-failed task", result.Tasks)
	}

	transitions := fx.Transitions.All(workflowScopeID)
	if len(transitions) != 4 {
		t.Fatalf("transitions = %d, want 4 (RUNNING, WAITING/budget, RUNNING, SUCCEEDED); got %+v", len(transitions), transitions)
	}
	if transitions[0].Status != state.StatusRunning {
		t.Fatalf("transitions[0].Status = %q, want RUNNING", transitions[0].Status)
	}
	if transitions[1].Status != state.StatusWaiting || transitions[1].Reason != state.ReasonBudget {
		t.Fatalf("transitions[1] = %+v, want WAITING/budget", transitions[1])
	}
	if transitions[2].Status != state.StatusRunning || transitions[2].Reason != "" {
		t.Fatalf("transitions[2] = %+v, want RUNNING with no reason (resumed)", transitions[2])
	}
	if transitions[3].Status != state.StatusSucceeded {
		t.Fatalf("transitions[3].Status = %q, want SUCCEEDED", transitions[3].Status)
	}
}
