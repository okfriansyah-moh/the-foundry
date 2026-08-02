package mission

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestImprovementLoop_FrozenHalts(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ImprovementLoop)
	env.ExecuteWorkflow(ImprovementLoop, ImprovementLoopInput{
		MissionID: "m1", ProductID: "p1", Frozen: true, FrozenReason: "change-budget",
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("not complete")
	}
	var out ImprovementLoopResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if out.ResultCode != "FROZEN" {
		t.Fatalf("got %s", out.ResultCode)
	}
}

func TestImprovementLoop_HappyRetain(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	acts := &ImprovementActivities{
		AcquireLease: func(context.Context, ImprovementLoopInput) (ImprovementLeaseResult, error) {
			return ImprovementLeaseResult{Acquired: true, LeaseID: "L1"}, nil
		},
		Generate: func(context.Context, ImprovementLoopInput) (ImprovementProposalResult, error) {
			return ImprovementProposalResult{PlanBytes: []byte("plan"), PlanDigest: "pd"}, nil
		},
		Admit: func(context.Context, ImprovementAdmissionInput) (ImprovementAdmissionResult, error) {
			return ImprovementAdmissionResult{Admitted: true, PlanID: "ap1", EnvelopeDigest: "ed"}, nil
		},
		StartDeliver: func(context.Context, ImprovementDeliveryInput) (ImprovementDeliveryResult, error) {
			return ImprovementDeliveryResult{WorkflowID: "del-1", DeployReceipt: "dep-1"}, nil
		},
		Observe: func(context.Context, ImprovementLoopInput) (ImprovementObservationResult, error) {
			return ImprovementObservationResult{Beneficial: true, HealthOK: true}, nil
		},
	}
	env.RegisterWorkflow(ImprovementLoop)
	env.RegisterActivity(acts.AcquireImprovementLease)
	env.RegisterActivity(acts.GenerateImprovementPlan)
	env.RegisterActivity(acts.ResolveImprovementAdmission)
	env.RegisterActivity(acts.RecordImprovementRun)
	env.RegisterActivity(acts.StartImprovementDelivery)
	env.RegisterActivity(acts.ObservePostChange)
	env.RegisterActivity(acts.DecideRetainOrRollback)

	env.ExecuteWorkflow(ImprovementLoop, ImprovementLoopInput{MissionID: "m1", ProductID: "p1", CycleID: "c1"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out ImprovementLoopResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Retained || out.ResultCode != "IMPROVEMENT_RETAINED" {
		t.Fatalf("%+v", out)
	}
}

func TestImprovementLoop_RollbackOnBadHealth(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	acts := &ImprovementActivities{
		AcquireLease: func(context.Context, ImprovementLoopInput) (ImprovementLeaseResult, error) {
			return ImprovementLeaseResult{Acquired: true, LeaseID: "L1"}, nil
		},
		Generate: func(context.Context, ImprovementLoopInput) (ImprovementProposalResult, error) {
			return ImprovementProposalResult{PlanBytes: []byte("plan"), PlanDigest: "pd"}, nil
		},
		Admit: func(context.Context, ImprovementAdmissionInput) (ImprovementAdmissionResult, error) {
			return ImprovementAdmissionResult{Admitted: true, PlanID: "ap1", EnvelopeDigest: "ed"}, nil
		},
		StartDeliver: func(context.Context, ImprovementDeliveryInput) (ImprovementDeliveryResult, error) {
			return ImprovementDeliveryResult{WorkflowID: "del-1"}, nil
		},
		Observe: func(context.Context, ImprovementLoopInput) (ImprovementObservationResult, error) {
			return ImprovementObservationResult{Beneficial: false, HealthOK: false}, nil
		},
	}
	env.RegisterWorkflow(ImprovementLoop)
	env.RegisterActivity(acts.AcquireImprovementLease)
	env.RegisterActivity(acts.GenerateImprovementPlan)
	env.RegisterActivity(acts.ResolveImprovementAdmission)
	env.RegisterActivity(acts.RecordImprovementRun)
	env.RegisterActivity(acts.StartImprovementDelivery)
	env.RegisterActivity(acts.ObservePostChange)
	env.RegisterActivity(acts.DecideRetainOrRollback)

	env.ExecuteWorkflow(ImprovementLoop, ImprovementLoopInput{MissionID: "m1", ProductID: "p1", CycleID: "c2"})
	var out ImprovementLoopResult
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	if out.Retained || out.ResultCode != "IMPROVEMENT_ROLLED_BACK" {
		t.Fatalf("%+v", out)
	}
}
