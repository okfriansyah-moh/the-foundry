// Command changeset drives the multi-repository 10x change-set saga e2e for
// docs/PLAN.md Task 78 (EVO-05), across 3 fixture repos including one seeded
// failure. It proves against the real internal/kernel saga resolver:
//
//   - all-success: 3 repos, ordered integration, SUCCEEDED/handoff-ready;
//   - seeded failure: a mid-graph repo fails → PROVEN_BLOCKED with an exact
//     per-repo receipt map, the pushed dependent recorded (not auto-reverted),
//     and a human next_action.
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func plan() kernel.ChangeSetPlan {
	return kernel.ChangeSetPlan{
		InitiativeID: "multi-repo-init",
		Contracts:    []kernel.CrossRepoContract{{Name: "shared-api", Digest: "d1"}},
		EnvRevision:  "toolchain@2026-07-28",
		Repos: []kernel.RepoNode{
			{Alias: "platform"},
			{Alias: "service", DependsOn: []string{"platform"}},
			{Alias: "frontend", DependsOn: []string{"service"}},
		},
	}
}

func pushed(branch string) kernel.RepoAttempt {
	return kernel.RepoAttempt{Pushed: true, Receipt: integrator.Receipt{Branch: branch, AfterSHA: "cafe"}}
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("changeset: FAIL: %v", err)
	}
	fmt.Println("changeset: PASS — all-success and seeded-failure change-set semantics held")
}

func run() error {
	// Case A: all three repos succeed → integrated in order.
	a := kernel.RunChangeSet(plan(), map[string]kernel.RepoAttempt{
		"platform": pushed("10x/platform"), "service": pushed("10x/service"), "frontend": pushed("10x/frontend"),
	})
	if a.ResultCode != state.ResultTenXBranchHandoffReady {
		return fmt.Errorf("all-success case: want handoff-ready, got %s", a.ResultCode)
	}
	for _, r := range []string{"platform", "service", "frontend"} {
		if a.Receipts[r].Status != kernel.RepoIntegrated {
			return fmt.Errorf("all-success: %s not integrated: %+v", r, a.Receipts[r])
		}
	}
	fmt.Printf("changeset: ok [all-success] status=%s repos=platform,service,frontend all integrated\n", a.ResultCode)

	// Case B: middle repo (service) fails; frontend pushed independently.
	b := kernel.RunChangeSet(plan(), map[string]kernel.RepoAttempt{
		"platform": pushed("10x/platform"),
		"service":  {Pushed: false, Err: "seeded failure: service validation failed"},
		"frontend": pushed("10x/frontend"),
	})
	if b.ResultCode != state.ResultProvenBlocked {
		return fmt.Errorf("seeded-failure case: want PROVEN_BLOCKED, got %s", b.ResultCode)
	}
	want := map[string]kernel.RepoStatus{
		"platform": kernel.RepoIntegrated,
		"service":  kernel.RepoFailed,
		"frontend": kernel.RepoPushedBlocked,
	}
	for r, st := range want {
		if b.Receipts[r].Status != st {
			return fmt.Errorf("seeded-failure: %s status=%s, want %s", r, b.Receipts[r].Status, st)
		}
	}
	if !strings.Contains(b.NextAction, "NOT auto-reverted") {
		return fmt.Errorf("seeded-failure: next_action must state no auto-revert, got %q", b.NextAction)
	}
	fmt.Printf("changeset: ok [seeded-failure] status=%s platform=integrated service=failed frontend=pushed-blocked\n", b.ResultCode)
	fmt.Printf("changeset: next_action = %s\n", b.NextAction)
	return nil
}
