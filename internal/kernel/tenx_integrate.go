package kernel

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// Activity names for the 10x integration path (docs/PLAN.md Task 108 / RTC-04).
const (
	ActivitySelectBranchDeliveryPolicy = "SelectBranchDeliveryPolicy"
	ActivityIntegrateChangeSet         = "IntegrateChangeSet"
)

// IntegrationQueue is the durable per-branch integration queue plus receipt
// store IntegrateChangeSet depends on. *integrator.PGQueue satisfies it; the
// in-memory integrator.Queue is used by unit tests via an adapter.
type IntegrationQueue interface {
	Enqueue(ctx context.Context, item integrator.IntegrationItem) error
	Claim(ctx context.Context, branch string) (integrator.IntegrationItem, bool, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id, reason string) error
	RecordReceipt(ctx context.Context, r integrator.Receipt) error
	ReceiptForGroup(ctx context.Context, groupID, branch string) (integrator.Receipt, bool, error)
}

// IntegrateChangeSetInput is the activity's serializable input.
type IntegrateChangeSetInput struct {
	Item   integrator.IntegrationItem `json:"item"`
	Policy string                     `json:"policy"`
}

// IntegrateChangeSetOutput reports the push receipt and whether the push was
// short-circuited on a prior receipt (idempotent replay).
type IntegrateChangeSetOutput struct {
	Receipt           integrator.Receipt `json:"receipt"`
	AlreadyIntegrated bool               `json:"already_integrated"`
	Pushed            bool               `json:"pushed"`
}

// IntegrateChangeSet is the kernel activity that carries one atomic group's
// change-set to its remote: enqueue → claim → (drift-check → CAS push →
// receipt) → dequeue, all idempotent so a retry never double-pushes
// (docs/PLAN.md Task 108). It pushes only under the direct-shared-branch
// policy; no-remote-write records nothing and pull-request is impossible here
// (the workflow refuses it, Constitution C15). PushBranch remains the only
// internal/scm/write call site — IntegrateChangeSet reaches it through the
// injected Integrator's CASPusher, never by importing scm/write.
func (a *Activities) IntegrateChangeSet(ctx context.Context, in IntegrateChangeSetInput) (IntegrateChangeSetOutput, error) {
	if a.IntegrationQueue == nil || a.Integrator == nil {
		return IntegrateChangeSetOutput{}, fmt.Errorf("kernel: IntegrateChangeSet requires an integration queue and integrator")
	}
	policy := selectBranchDeliveryPolicy(in.Policy)
	if policy == PolicyPullRequest {
		return IntegrateChangeSetOutput{}, fmt.Errorf("kernel: 10x integration must never run under pull-request (Constitution C15)")
	}

	// Idempotency: a retried integration returns the recorded receipt without a
	// second push.
	if r, ok, err := a.IntegrationQueue.ReceiptForGroup(ctx, in.Item.GroupID, in.Item.Branch); err != nil {
		return IntegrateChangeSetOutput{}, err
	} else if ok {
		return IntegrateChangeSetOutput{Receipt: r, AlreadyIntegrated: true, Pushed: false}, nil
	}

	// no-remote-write: never touches the remote.
	if policy == PolicyNoRemoteWrite {
		return IntegrateChangeSetOutput{Pushed: false}, nil
	}

	if err := a.IntegrationQueue.Enqueue(ctx, in.Item); err != nil {
		return IntegrateChangeSetOutput{}, err
	}
	claimed, ok, err := a.IntegrationQueue.Claim(ctx, in.Item.Branch)
	if err != nil {
		return IntegrateChangeSetOutput{}, err
	}
	if !ok {
		return IntegrateChangeSetOutput{}, fmt.Errorf("kernel: nothing claimable for branch %s after enqueue", in.Item.Branch)
	}

	receipt, err := a.Integrator.ProcessItem(ctx, claimed)
	if err != nil {
		_ = a.IntegrationQueue.Fail(ctx, claimed.ID, err.Error())
		return IntegrateChangeSetOutput{}, fmt.Errorf("kernel: integrate change-set: %w", err)
	}
	if err := a.IntegrationQueue.RecordReceipt(ctx, receipt); err != nil {
		return IntegrateChangeSetOutput{}, err
	}
	if err := a.IntegrationQueue.Complete(ctx, claimed.ID); err != nil {
		return IntegrateChangeSetOutput{}, err
	}
	return IntegrateChangeSetOutput{Receipt: receipt, Pushed: true}, nil
}

// TenXDeliverInput is TenXDeliver's workflow input: the resolved org branch-
// delivery-policy layer value and the atomic-group change-sets to integrate
// (their tasks having been delivered through the same DeliverPlan path).
type TenXDeliverInput struct {
	OrgPolicyLayerValue string                       `json:"org_policy_layer_value"`
	ChangeSets          []integrator.IntegrationItem `json:"change_sets"`
	EvidenceLinks       []string                     `json:"evidence_links"`
}

// TenXDeliver is the real Temporal workflow that assembles the 10x branch
// handoff (docs/PLAN.md Task 108 / RTC-04): resolve the branch delivery policy
// (refuse pull-request), integrate each change-set through the durable
// Postgres queue, then terminate SUCCEEDED/TEN_X_BRANCH_HANDOFF_READY. It opens
// no PR, performs no merge, and reaches no staging/production deploy activity
// (Constitution C4/C15).
func TenXDeliver(ctx workflow.Context, in TenXDeliverInput) (TenXDeliverResult, error) {
	ao := workflow.ActivityOptions{StartToCloseTimeout: 2 * defaultLeaseTTL}
	actx := workflow.WithActivityOptions(ctx, ao)

	var policyOut SelectBranchDeliveryPolicyOutput
	if err := workflow.ExecuteActivity(actx, ActivitySelectBranchDeliveryPolicy, SelectBranchDeliveryPolicyInput{
		OrgLayerValue: in.OrgPolicyLayerValue,
	}).Get(actx, &policyOut); err != nil {
		return TenXDeliverResult{}, fmt.Errorf("tenx: select branch delivery policy: %w", err)
	}
	if !policyOut.TenXAllowed {
		return TenXDeliverResult{
			Status:     state.StatusFailed,
			ResultCode: state.ResultProvenBlocked,
			HandoffNote: fmt.Sprintf(
				"10x delivery refused under branch delivery policy %q — pull-request is prohibited (C15)", policyOut.Policy),
		}, nil
	}

	receipts := make([]integrator.Receipt, 0, len(in.ChangeSets))
	for _, cs := range in.ChangeSets {
		var out IntegrateChangeSetOutput
		if err := workflow.ExecuteActivity(actx, ActivityIntegrateChangeSet, IntegrateChangeSetInput{
			Item: cs, Policy: policyOut.Policy,
		}).Get(actx, &out); err != nil {
			return TenXDeliverResult{}, fmt.Errorf("tenx: integrate change-set %s: %w", cs.GroupID, err)
		}
		if out.Pushed || out.AlreadyIntegrated {
			receipts = append(receipts, out.Receipt)
		}
	}

	return TenXHandoffTerminal(receipts, in.EvidenceLinks), nil
}
