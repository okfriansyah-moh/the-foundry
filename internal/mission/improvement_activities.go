package mission

import (
	"context"
	"fmt"
)

// docs/PLAN.md Task 147: ImprovementLoop activity DTOs and default implementations.

type ImprovementLeaseResult struct {
	Acquired bool
	LeaseID  string
}

type ImprovementProposalResult struct {
	PlanBytes  []byte
	PlanDigest string
}

type ImprovementAdmissionInput struct {
	MissionID  string
	ProductID  string
	CycleID    string
	PlanBytes  []byte
	PlanDigest string
}

type ImprovementAdmissionResult struct {
	Admitted       bool
	RequiresHuman  bool
	HaltReason     string
	PlanID         string
	EnvelopeDigest string
	Tier           string
}

type ImprovementRunRecord struct {
	RunID          string
	MissionID      string
	ProductID      string
	LeaseID        string
	PlanID         string
	PlanDigest     string
	EnvelopeDigest string
	IdempotencyKey string
	Status         string
}

type ImprovementDeliveryInput struct {
	MissionID      string
	ProductID      string
	ApprovedPlanID string
	EnvelopeDigest string
	PlanDigest     string
	TaskQueue      string
}

type ImprovementDeliveryResult struct {
	WorkflowID    string
	DeployReceipt string
}

type ImprovementObservationResult struct {
	Beneficial bool
	HealthOK   bool
}

type ImprovementRetainInput struct {
	MissionID  string
	ProductID  string
	CycleID    string
	Beneficial bool
	HealthOK   bool
	DeployRef  string
}

type ImprovementRetainDecision struct {
	Retained    bool
	PromotionID string
	RollbackRef string
}

// ImprovementActivities hosts Task 147 side-effect seams.
type ImprovementActivities struct {
	// AcquireLease returns false when another improvement is in flight.
	AcquireLease func(ctx context.Context, in ImprovementLoopInput) (ImprovementLeaseResult, error)
	Generate     func(ctx context.Context, in ImprovementLoopInput) (ImprovementProposalResult, error)
	Admit        func(ctx context.Context, in ImprovementAdmissionInput) (ImprovementAdmissionResult, error)
	Record       func(ctx context.Context, in ImprovementRunRecord) error
	StartDeliver func(ctx context.Context, in ImprovementDeliveryInput) (ImprovementDeliveryResult, error)
	Observe      func(ctx context.Context, in ImprovementLoopInput) (ImprovementObservationResult, error)
	Decide       func(ctx context.Context, in ImprovementRetainInput) (ImprovementRetainDecision, error)
}

func (a *ImprovementActivities) AcquireImprovementLease(ctx context.Context, in ImprovementLoopInput) (ImprovementLeaseResult, error) {
	if a.AcquireLease == nil {
		return ImprovementLeaseResult{}, fmt.Errorf("mission: AcquireLease not configured")
	}
	return a.AcquireLease(ctx, in)
}

func (a *ImprovementActivities) GenerateImprovementPlan(ctx context.Context, in ImprovementLoopInput) (ImprovementProposalResult, error) {
	if a.Generate == nil {
		return ImprovementProposalResult{}, fmt.Errorf("mission: Generate not configured")
	}
	return a.Generate(ctx, in)
}

func (a *ImprovementActivities) ResolveImprovementAdmission(ctx context.Context, in ImprovementAdmissionInput) (ImprovementAdmissionResult, error) {
	if a.Admit == nil {
		return ImprovementAdmissionResult{}, fmt.Errorf("mission: Admit not configured")
	}
	return a.Admit(ctx, in)
}

func (a *ImprovementActivities) RecordImprovementRun(ctx context.Context, in ImprovementRunRecord) error {
	if a.Record == nil {
		return nil
	}
	return a.Record(ctx, in)
}

func (a *ImprovementActivities) StartImprovementDelivery(ctx context.Context, in ImprovementDeliveryInput) (ImprovementDeliveryResult, error) {
	if a.StartDeliver == nil {
		return ImprovementDeliveryResult{}, fmt.Errorf("mission: StartDeliver not configured")
	}
	return a.StartDeliver(ctx, in)
}

func (a *ImprovementActivities) ObservePostChange(ctx context.Context, in ImprovementLoopInput) (ImprovementObservationResult, error) {
	if a.Observe == nil {
		return ImprovementObservationResult{Beneficial: true, HealthOK: true}, nil
	}
	return a.Observe(ctx, in)
}

func (a *ImprovementActivities) DecideRetainOrRollback(ctx context.Context, in ImprovementRetainInput) (ImprovementRetainDecision, error) {
	if a.Decide != nil {
		return a.Decide(ctx, in)
	}
	if !in.HealthOK || !in.Beneficial {
		return ImprovementRetainDecision{Retained: false, RollbackRef: "rollback:" + in.CycleID}, nil
	}
	return ImprovementRetainDecision{Retained: true, PromotionID: "promo:" + in.CycleID}, nil
}
