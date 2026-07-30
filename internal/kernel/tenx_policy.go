package kernel

import (
	"context"
	"fmt"
	"strings"
)

// BranchDeliveryPolicy is the kernel-owned, org-policy-derived decision of how
// an atomic group's change-set reaches its remote (docs/PLAN.md Task 108 /
// RTC-04; multi-repository.md §N10.2). The 10x workflow refuses to run under
// pull-request (Constitution C15 forbids the kernel opening PRs).
type BranchDeliveryPolicy string

// The three branch delivery policies.
const (
	PolicyNoRemoteWrite      BranchDeliveryPolicy = "no-remote-write"
	PolicyDirectSharedBranch BranchDeliveryPolicy = "direct-shared-branch"
	PolicyPullRequest        BranchDeliveryPolicy = "pull-request"
)

// Valid reports whether p is one of the three recognized policies.
func (p BranchDeliveryPolicy) Valid() bool {
	switch p {
	case PolicyNoRemoteWrite, PolicyDirectSharedBranch, PolicyPullRequest:
		return true
	default:
		return false
	}
}

// selectBranchDeliveryPolicy resolves the compiled org-layer value to a policy,
// failing closed to no-remote-write when the org layer names none or names an
// unrecognized value — the kernel never guesses a write policy.
func selectBranchDeliveryPolicy(orgLayerValue string) BranchDeliveryPolicy {
	p := BranchDeliveryPolicy(strings.TrimSpace(strings.ToLower(orgLayerValue)))
	if !p.Valid() {
		return PolicyNoRemoteWrite
	}
	return p
}

// SelectBranchDeliveryPolicyInput/Output are the activity's serializable shapes.
type SelectBranchDeliveryPolicyInput struct {
	OrgLayerValue string `json:"org_layer_value"`
}

// SelectBranchDeliveryPolicyOutput carries the resolved policy and whether the
// 10x workflow may run under it.
type SelectBranchDeliveryPolicyOutput struct {
	Policy      string `json:"policy"`
	TenXAllowed bool   `json:"tenx_allowed"`
}

// SelectBranchDeliveryPolicy is the kernel activity: deterministic resolution
// from the compiled org policy, fail-closed to no-remote-write. It reports
// whether the 10x workflow may run under the resolved policy — never under
// pull-request (Constitution C15).
func (a *Activities) SelectBranchDeliveryPolicy(_ context.Context, in SelectBranchDeliveryPolicyInput) (SelectBranchDeliveryPolicyOutput, error) {
	p := selectBranchDeliveryPolicy(in.OrgLayerValue)
	return SelectBranchDeliveryPolicyOutput{
		Policy:      string(p),
		TenXAllowed: p != PolicyPullRequest,
	}, nil
}

// Push-cadence vocabulary (docs/PLAN.md Task 108; ten-x-branch.md /
// multi-repository.md §N10.2). The canonical default is after-atomic-group;
// after-accepted-task is permitted only under the exact
// buildable-and-testable intermediate-branch invariant.
const (
	CadenceAfterAtomicGroup  = "after-atomic-group"
	CadenceAfterAcceptedTask = "after-accepted-task"

	InvariantBuildableAndTestable = "buildable-and-testable"
)

// DefaultPushCadence is the canonical cadence a config or workflow default must
// not drift away from.
const DefaultPushCadence = CadenceAfterAtomicGroup

// ValidatePushCadence enforces the single cadence rule: after-atomic-group is
// always valid; after-accepted-task is valid only when the intermediate-branch
// invariant is exactly buildable-and-testable; anything else is refused rather
// than silently accepted (docs/PLAN.md Task 108 Step 7).
func ValidatePushCadence(cadence, intermediateBranchInvariant string) error {
	switch strings.TrimSpace(cadence) {
	case CadenceAfterAtomicGroup:
		return nil
	case CadenceAfterAcceptedTask:
		if strings.TrimSpace(intermediateBranchInvariant) != InvariantBuildableAndTestable {
			return fmt.Errorf("kernel: push cadence %q requires intermediate_branch_invariant %q, got %q",
				CadenceAfterAcceptedTask, InvariantBuildableAndTestable, intermediateBranchInvariant)
		}
		return nil
	case "":
		return fmt.Errorf("kernel: push cadence is required (default %q)", DefaultPushCadence)
	default:
		return fmt.Errorf("kernel: unknown push cadence %q", cadence)
	}
}
