package mission

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// docs/PLAN.md Task 147 (VEN-19): ImprovementLoop child workflow.

const (
	ActivityAcquireImprovementLease     = "AcquireImprovementLease"
	ActivityGenerateImprovementPlan     = "GenerateImprovementPlan"
	ActivityResolveImprovementAdmission = "ResolveImprovementAdmission"
	ActivityRecordImprovementRun        = "RecordImprovementRun"
	ActivityObservePostChange           = "ObservePostChange"
	ActivityDecideRetainOrRollback      = "DecideRetainOrRollback"
	ActivityStartImprovementDelivery    = "StartImprovementDelivery"
)

// ImprovementLoopInput starts one bounded improvement cycle for a product.
type ImprovementLoopInput struct {
	MissionID       string
	ProductID       string
	CycleID         string
	IdempotencyKey  string
	BudgetCapUSD    float64
	Frozen          bool
	FrozenReason    string
	DeliveryTaskQueue string
	// ApprovedPlanID / EnvelopeDigest are filled by activities after admission.
	ApprovedPlanID  string
	EnvelopeDigest  string
	PlanDigest      string
}

// ImprovementLoopResult is the terminal outcome of ImprovementLoop.
type ImprovementLoopResult struct {
	Status       string
	ResultCode   string
	PromotionID  string
	RollbackRef  string
	DeliveryWF   string
	Retained     bool
}

// ImprovementLoop sequences lease → proposal → admission → DeliverPlan child →
// deploy/observe → retain-or-rollback. All I/O is activity-based.
func ImprovementLoop(ctx workflow.Context, in ImprovementLoopInput) (ImprovementLoopResult, error) {
	if in.MissionID == "" || in.ProductID == "" {
		return ImprovementLoopResult{}, fmt.Errorf("mission: ImprovementLoop requires mission_id and product_id")
	}
	if in.Frozen {
		return ImprovementLoopResult{Status: "WAITING", ResultCode: "FROZEN", RollbackRef: in.FrozenReason}, nil
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var lease ImprovementLeaseResult
	if err := workflow.ExecuteActivity(ctx, ActivityAcquireImprovementLease, in).Get(ctx, &lease); err != nil {
		return ImprovementLoopResult{}, err
	}
	if !lease.Acquired {
		return ImprovementLoopResult{Status: "WAITING", ResultCode: "LEASE_HELD"}, nil
	}

	var proposal ImprovementProposalResult
	if err := workflow.ExecuteActivity(ctx, ActivityGenerateImprovementPlan, in).Get(ctx, &proposal); err != nil {
		return ImprovementLoopResult{}, err
	}

	var admission ImprovementAdmissionResult
	if err := workflow.ExecuteActivity(ctx, ActivityResolveImprovementAdmission, ImprovementAdmissionInput{
		MissionID: in.MissionID,
		ProductID: in.ProductID,
		CycleID:   in.CycleID,
		PlanBytes: proposal.PlanBytes,
		PlanDigest: proposal.PlanDigest,
	}).Get(ctx, &admission); err != nil {
		return ImprovementLoopResult{}, err
	}
	if admission.RequiresHuman {
		return ImprovementLoopResult{Status: "WAITING", ResultCode: "H_TIER_APPROVAL"}, nil
	}
	if !admission.Admitted {
		return ImprovementLoopResult{Status: "WAITING", ResultCode: admission.HaltReason}, nil
	}

	_ = workflow.ExecuteActivity(ctx, ActivityRecordImprovementRun, ImprovementRunRecord{
		RunID:           in.CycleID,
		MissionID:       in.MissionID,
		ProductID:       in.ProductID,
		LeaseID:         lease.LeaseID,
		PlanID:          admission.PlanID,
		PlanDigest:      proposal.PlanDigest,
		EnvelopeDigest:  admission.EnvelopeDigest,
		IdempotencyKey:  in.IdempotencyKey,
		Status:          "delivering",
	}).Get(ctx, nil)

	// DeliverPlan is invoked by a kernel activity in production; the workflow
	// records the child workflow id returned by that activity seam.
	var delivery ImprovementDeliveryResult
	if err := workflow.ExecuteActivity(ctx, ActivityStartImprovementDelivery, ImprovementDeliveryInput{
		MissionID:      in.MissionID,
		ProductID:      in.ProductID,
		ApprovedPlanID: admission.PlanID,
		EnvelopeDigest: admission.EnvelopeDigest,
		PlanDigest:     proposal.PlanDigest,
		TaskQueue:      in.DeliveryTaskQueue,
	}).Get(ctx, &delivery); err != nil {
		return ImprovementLoopResult{}, err
	}

	var post ImprovementObservationResult
	if err := workflow.ExecuteActivity(ctx, ActivityObservePostChange, in).Get(ctx, &post); err != nil {
		return ImprovementLoopResult{}, err
	}

	var decision ImprovementRetainDecision
	if err := workflow.ExecuteActivity(ctx, ActivityDecideRetainOrRollback, ImprovementRetainInput{
		MissionID:   in.MissionID,
		ProductID:   in.ProductID,
		CycleID:     in.CycleID,
		Beneficial:  post.Beneficial,
		HealthOK:    post.HealthOK,
		DeployRef:   delivery.DeployReceipt,
	}).Get(ctx, &decision); err != nil {
		return ImprovementLoopResult{}, err
	}

	status := "SUCCEEDED"
	code := "IMPROVEMENT_RETAINED"
	if !decision.Retained {
		status = "SUCCEEDED"
		code = "IMPROVEMENT_ROLLED_BACK"
	}
	return ImprovementLoopResult{
		Status:      status,
		ResultCode:  code,
		PromotionID: decision.PromotionID,
		RollbackRef: decision.RollbackRef,
		DeliveryWF:  delivery.WorkflowID,
		Retained:    decision.Retained,
	}, nil
}
