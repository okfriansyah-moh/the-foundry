package kernel

import (
	"context"
	"testing"
)

func TestSelectBranchDeliveryPolicyFailsClosed(t *testing.T) {
	cases := map[string]BranchDeliveryPolicy{
		"":                         PolicyNoRemoteWrite,
		"unknown":                  PolicyNoRemoteWrite,
		"no-remote-write":          PolicyNoRemoteWrite,
		"direct-shared-branch":     PolicyDirectSharedBranch,
		"pull-request":             PolicyPullRequest,
		"  Direct-Shared-Branch  ": PolicyDirectSharedBranch,
	}
	for in, want := range cases {
		if got := selectBranchDeliveryPolicy(in); got != want {
			t.Fatalf("selectBranchDeliveryPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectBranchDeliveryPolicyActivityRefusesPRForTenX(t *testing.T) {
	a := &Activities{}
	out, err := a.SelectBranchDeliveryPolicy(context.Background(), SelectBranchDeliveryPolicyInput{OrgLayerValue: "pull-request"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Policy != string(PolicyPullRequest) || out.TenXAllowed {
		t.Fatalf("pull-request must be resolved but disallowed for 10x: %+v", out)
	}
	out, _ = a.SelectBranchDeliveryPolicy(context.Background(), SelectBranchDeliveryPolicyInput{OrgLayerValue: ""})
	if out.Policy != string(PolicyNoRemoteWrite) || !out.TenXAllowed {
		t.Fatalf("empty must fail closed to no-remote-write and be allowed: %+v", out)
	}
}

func TestValidatePushCadence(t *testing.T) {
	if err := ValidatePushCadence(CadenceAfterAtomicGroup, ""); err != nil {
		t.Fatalf("after-atomic-group must be valid: %v", err)
	}
	if err := ValidatePushCadence(CadenceAfterAcceptedTask, InvariantBuildableAndTestable); err != nil {
		t.Fatalf("after-accepted-task with the invariant must be valid: %v", err)
	}
	if err := ValidatePushCadence(CadenceAfterAcceptedTask, ""); err == nil {
		t.Fatal("after-accepted-task without the invariant must be refused")
	}
	if err := ValidatePushCadence(CadenceAfterAcceptedTask, "some-other-invariant"); err == nil {
		t.Fatal("after-accepted-task with a wrong invariant must be refused")
	}
	if err := ValidatePushCadence("", ""); err == nil {
		t.Fatal("empty cadence must be refused")
	}
	if err := ValidatePushCadence("bogus", ""); err == nil {
		t.Fatal("unknown cadence must be refused")
	}
}

func TestDefaultPushCadenceIsAfterAtomicGroup(t *testing.T) {
	if DefaultPushCadence != CadenceAfterAtomicGroup {
		t.Fatalf("default cadence drifted to %q", DefaultPushCadence)
	}
}
