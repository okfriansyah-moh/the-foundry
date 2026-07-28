package kernel

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func linearPlan() ChangeSetPlan {
	return ChangeSetPlan{
		InitiativeID: "init-1",
		Contracts:    []CrossRepoContract{{Name: "api", Digest: "abc"}},
		EnvRevision:  "toolchain@2026-07-28",
		Repos: []RepoNode{
			{Alias: "core"},
			{Alias: "svc", DependsOn: []string{"core"}},
			{Alias: "web", DependsOn: []string{"svc"}},
		},
	}
}

func pushed(branch string) RepoAttempt {
	return RepoAttempt{Pushed: true, Receipt: integrator.Receipt{Branch: branch, AfterSHA: "deadbeef"}}
}

func TestChangeSet_AllSucceed(t *testing.T) {
	res := RunChangeSet(linearPlan(), map[string]RepoAttempt{
		"core": pushed("10x/core"), "svc": pushed("10x/svc"), "web": pushed("10x/web"),
	})
	if res.Status != state.StatusSucceeded || res.ResultCode != state.ResultTenXBranchHandoffReady {
		t.Fatalf("want SUCCEEDED/handoff-ready, got %s/%s", res.Status, res.ResultCode)
	}
	for _, alias := range []string{"core", "svc", "web"} {
		if res.Receipts[alias].Status != RepoIntegrated {
			t.Fatalf("%s not integrated: %+v", alias, res.Receipts[alias])
		}
	}
	if res.EnvRevision != "toolchain@2026-07-28" {
		t.Fatalf("env revision provenance lost: %q", res.EnvRevision)
	}
	if len(res.ContractDigests) != 1 || res.ContractDigests[0] != "api@abc" {
		t.Fatalf("contract freeze wrong: %v", res.ContractDigests)
	}
}

// TestChangeSet_SeededFailure_PartialReceiptMapExact is the acceptance case:
// a mid-graph failure ends PROVEN_BLOCKED with an exact per-repo receipt map;
// the pushed-but-blocked dependent is recorded (never auto-reverted).
func TestChangeSet_SeededFailure_PartialReceiptMapExact(t *testing.T) {
	res := RunChangeSet(linearPlan(), map[string]RepoAttempt{
		"core": pushed("10x/core"),
		"svc":  {Pushed: false, Err: "validation failed on svc"},
		"web":  pushed("10x/web"), // pushed independently, but its dep svc failed
	})
	if res.Status != state.StatusFailed || res.ResultCode != state.ResultProvenBlocked {
		t.Fatalf("want FAILED/PROVEN_BLOCKED, got %s/%s", res.Status, res.ResultCode)
	}
	if res.Receipts["core"].Status != RepoIntegrated {
		t.Fatalf("core should be integrated (isolation): %+v", res.Receipts["core"])
	}
	if res.Receipts["svc"].Status != RepoFailed || res.Receipts["svc"].Error == "" {
		t.Fatalf("svc should be failed with error: %+v", res.Receipts["svc"])
	}
	// web pushed but its dependency failed → pushed-blocked, NOT integrated,
	// NOT reverted (ordered integration respected).
	if res.Receipts["web"].Status != RepoPushedBlocked {
		t.Fatalf("web should be pushed-blocked (ordered integration): %+v", res.Receipts["web"])
	}
	if res.NextAction == "" {
		t.Fatal("PROVEN_BLOCKED must carry a human next_action")
	}
}

// TestChangeSet_NoAutoRevertLanguage checks the next_action explicitly states
// pushed branches were not auto-reverted (humans own remediation).
func TestChangeSet_NoAutoRevertLanguage(t *testing.T) {
	res := RunChangeSet(linearPlan(), map[string]RepoAttempt{
		"core": pushed("10x/core"),
		"svc":  {Pushed: false, Err: "boom"},
		"web":  {Pushed: false, Err: "boom"},
	})
	if got := res.NextAction; got == "" || !contains(got, "NOT auto-reverted") {
		t.Fatalf("next_action must state no auto-revert, got %q", got)
	}
}

func TestChangeSet_CycleFailsClosed(t *testing.T) {
	plan := ChangeSetPlan{
		InitiativeID: "cyc",
		Repos: []RepoNode{
			{Alias: "a", DependsOn: []string{"b"}},
			{Alias: "b", DependsOn: []string{"a"}},
		},
	}
	res := RunChangeSet(plan, map[string]RepoAttempt{"a": pushed("x"), "b": pushed("y")})
	if res.ResultCode != state.ResultProvenBlocked {
		t.Fatalf("cycle must fail closed PROVEN_BLOCKED, got %s", res.ResultCode)
	}
	if !contains(res.NextAction, "cycle") {
		t.Fatalf("next_action should mention the cycle: %q", res.NextAction)
	}
}

func TestFreezeContracts_Deterministic(t *testing.T) {
	c := []CrossRepoContract{{Name: "b", Digest: "2"}, {Name: "a", Digest: "1"}}
	d1, h1 := FreezeContracts(c)
	d2, h2 := FreezeContracts([]CrossRepoContract{{Name: "a", Digest: "1"}, {Name: "b", Digest: "2"}})
	if h1 != h2 {
		t.Fatalf("freeze digest not order-independent: %s vs %s", h1, h2)
	}
	if len(d1) != 2 || d1[0] != "a@1" {
		t.Fatalf("frozen digests not sorted: %v", d1)
	}
	_ = d2
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
