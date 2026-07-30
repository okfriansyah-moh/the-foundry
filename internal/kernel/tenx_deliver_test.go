package kernel_test

import (
	"context"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func registerTenXFakes(env *testsuite.TestWorkflowEnvironment, policy string, allowed bool) {
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ kernel.SelectBranchDeliveryPolicyInput) (kernel.SelectBranchDeliveryPolicyOutput, error) {
			return kernel.SelectBranchDeliveryPolicyOutput{Policy: policy, TenXAllowed: allowed}, nil
		},
		activity.RegisterOptions{Name: kernel.ActivitySelectBranchDeliveryPolicy},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in kernel.IntegrateChangeSetInput) (kernel.IntegrateChangeSetOutput, error) {
			return kernel.IntegrateChangeSetOutput{
				Receipt: integrator.Receipt{Branch: in.Item.Branch, GroupID: in.Item.GroupID, AfterSHA: "sha-after", ManifestDigest: in.Item.ManifestDigest},
				Pushed:  true,
			}, nil
		},
		activity.RegisterOptions{Name: kernel.ActivityIntegrateChangeSet},
	)
}

func TestTenXDeliver_RefusesPullRequest(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerTenXFakes(env, string(kernel.PolicyPullRequest), false)
	env.RegisterWorkflow(kernel.TenXDeliver)

	env.ExecuteWorkflow(kernel.TenXDeliver, kernel.TenXDeliverInput{
		OrgPolicyLayerValue: "pull-request",
		ChangeSets:          []integrator.IntegrationItem{{ID: "i1", Branch: "b1", GroupID: "g1"}},
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var res kernel.TenXDeliverResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Status != state.StatusFailed {
		t.Fatalf("pull-request must not deliver; got status %q", res.Status)
	}
}

func TestTenXDeliver_HandoffReady(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerTenXFakes(env, string(kernel.PolicyDirectSharedBranch), true)
	env.RegisterWorkflow(kernel.TenXDeliver)

	env.ExecuteWorkflow(kernel.TenXDeliver, kernel.TenXDeliverInput{
		OrgPolicyLayerValue: "direct-shared-branch",
		ChangeSets: []integrator.IntegrationItem{
			{ID: "i1", Branch: "b1", GroupID: "g1", ManifestDigest: "d1"},
		},
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	var res kernel.TenXDeliverResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Status != state.StatusSucceeded || res.ResultCode != state.ResultTenXBranchHandoffReady {
		t.Fatalf("expected SUCCEEDED/TEN_X_BRANCH_HANDOFF_READY, got %q/%q", res.Status, res.ResultCode)
	}
	if len(res.Receipts) != 1 {
		t.Fatalf("expected one receipt, got %d", len(res.Receipts))
	}
}
