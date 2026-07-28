// Command skillevolve drives the L1 skill-evolution both-path e2e for
// docs/PLAN.md Task 77 (EVO-04). It proves, against the real
// internal/evolve L1 pipeline:
//
//   - a clean prompt improvement on the personal profile flows through every
//     gate to promotion (registry version bump, previous retained);
//   - a permission-expanding candidate is rejected at the L1 gate;
//   - an org-profile candidate that clears every gate is proposal-only (H),
//     never auto-applied.
package main

import (
	"fmt"
	"log"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

func base() evolve.SkillVersion {
	return evolve.SkillVersion{
		SkillID: "code-review", Version: 1, Prompt: "review the diff",
		Permissions: []string{"read"}, DataClasses: []string{"code"}, BudgetUSD: 1.0,
	}
}

func suite() evolve.GoldenSuite {
	return evolve.GoldenSuite{Tasks: []evolve.GoldenTask{
		{Name: "non-empty-prompt", Check: func(v evolve.SkillVersion) bool { return len(v.Prompt) > 0 }},
	}}
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("skillevolve: FAIL: %v", err)
	}
	fmt.Println("skillevolve: PASS — L1 promotion, L1-gate rejection, and org proposal-only all held")
}

func run() error {
	evolve.Unfreeze()

	// Path 1: personal prompt improvement flows to promotion.
	reg := evolve.NewSkillRegistry()
	reg.Register(base())
	improved := base()
	improved.Prompt = "review the diff carefully and cite exact lines"
	p := evolve.L1Pipeline{Registry: reg, Suite: suite(), Limits: evolve.DefaultChangeBudgetLimits()}
	out := p.Evaluate(evolve.L1Candidate{Base: base(), Proposed: improved, Profile: "personal"}, evolve.L1Stages{ShadowPass: true, CanaryPass: true})
	if !out.Applied || out.Record.Stage != evolve.StagePromoted {
		return fmt.Errorf("personal prompt improvement did not promote: %+v", out)
	}
	if cur, _ := reg.Current("code-review"); cur.Version != 2 {
		return fmt.Errorf("registry not bumped to v2: v%d", cur.Version)
	}
	if len(reg.History("code-review")) != 2 {
		return fmt.Errorf("previous version not retained")
	}
	fmt.Printf("skillevolve: ok [personal-promotion] stage=%s applied=%v\n", out.Record.Stage, out.Applied)

	// Path 2: permission-expanding candidate rejected at the L1 gate.
	reg2 := evolve.NewSkillRegistry()
	reg2.Register(base())
	expand := base()
	expand.Permissions = []string{"read", "write"}
	p2 := evolve.L1Pipeline{Registry: reg2, Suite: suite(), Limits: evolve.DefaultChangeBudgetLimits()}
	out2 := p2.Evaluate(evolve.L1Candidate{Base: base(), Proposed: expand, Profile: "personal"}, evolve.L1Stages{ShadowPass: true, CanaryPass: true})
	if out2.Applied || out2.ConditionFailure != evolve.L1NewPermission {
		return fmt.Errorf("permission-expanding candidate not rejected at L1 gate: %+v", out2)
	}
	if cur, _ := reg2.Current("code-review"); cur.Version != 1 {
		return fmt.Errorf("rejected candidate wrongly bumped registry to v%d", cur.Version)
	}
	fmt.Printf("skillevolve: ok [l1-gate-rejection] failure=%s applied=%v\n", out2.ConditionFailure, out2.Applied)

	// Path 3: org profile clean candidate is proposal-only.
	reg3 := evolve.NewSkillRegistry()
	reg3.Register(base())
	orgImproved := base()
	orgImproved.Prompt = "review the diff thoroughly"
	p3 := evolve.L1Pipeline{Registry: reg3, Suite: suite(), Limits: evolve.DefaultChangeBudgetLimits()}
	out3 := p3.Evaluate(evolve.L1Candidate{Base: base(), Proposed: orgImproved, Profile: "org"}, evolve.L1Stages{ShadowPass: true, CanaryPass: true})
	if out3.Applied || !out3.ProposalOnly || out3.Record.Stage != evolve.StageProposed {
		return fmt.Errorf("org candidate not proposal-only: %+v", out3)
	}
	if cur, _ := reg3.Current("code-review"); cur.Version != 1 {
		return fmt.Errorf("org proposal wrongly bumped registry to v%d", cur.Version)
	}
	fmt.Printf("skillevolve: ok [org-proposal-only] stage=%s proposalOnly=%v\n", out3.Record.Stage, out3.ProposalOnly)
	return nil
}
