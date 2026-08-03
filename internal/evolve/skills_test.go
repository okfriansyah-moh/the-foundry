package evolve

import "testing"

func baseVersion() SkillVersion {
	return SkillVersion{
		SkillID:     "code-review",
		Version:     1,
		Prompt:      "review the diff",
		Permissions: []string{"read"},
		DataClasses: []string{"code"},
		BudgetUSD:   1.0,
	}
}

func passingSuite() GoldenSuite {
	return GoldenSuite{Tasks: []GoldenTask{
		{Name: "mentions-review", Check: func(v SkillVersion) bool { return len(v.Prompt) > 0 }},
	}}
}

// TestL1ConditionGateMatrix is Task 77's condition-gate matrix: each L1
// drift-governance condition rejects a violating candidate at the gate.
func TestL1ConditionGateMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SkillVersion)
		want   L1ConditionFailure
	}{
		{"clean", func(v *SkillVersion) { v.Prompt = "review the diff carefully" }, L1OK},
		{"new-permission", func(v *SkillVersion) { v.Permissions = []string{"read", "write"} }, L1NewPermission},
		{"new-data-class", func(v *SkillVersion) { v.DataClasses = []string{"code", "customer-pii"} }, L1NewDataClass},
		{"budget-increase", func(v *SkillVersion) { v.BudgetUSD = 5.0 }, L1BudgetIncrease},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposed := baseVersion()
			tc.mutate(&proposed)
			got := CheckL1Conditions(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "personal"})
			if got != tc.want {
				t.Fatalf("CheckL1Conditions = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestL1NotReversible rejects a candidate with no retained base version.
func TestL1NotReversible(t *testing.T) {
	if got := CheckL1Conditions(L1Candidate{Base: SkillVersion{}, Proposed: baseVersion()}); got != L1NotReversible {
		t.Fatalf("want not-reversible, got %q", got)
	}
}

// TestL1Promote_PersonalHappyPath: a clean prompt improvement flows through
// every gate to promotion, bumping the registry version and retaining the
// previous one.
func TestL1Promote_PersonalHappyPath(t *testing.T) {
	Unfreeze()
	reg := NewSkillRegistry()
	reg.Register(baseVersion())
	proposed := baseVersion()
	proposed.Prompt = "review the diff carefully and cite lines"

	p := L1Pipeline{Registry: reg, Suite: passingSuite(), Limits: DefaultChangeBudgetLimits()}
	out := p.Evaluate(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "personal"}, L1Stages{ShadowPass: true, CanaryPass: true})
	if !out.Applied || out.Record.Stage != StagePromoted {
		t.Fatalf("expected promotion, got %+v", out)
	}
	cur, _ := reg.Current("code-review")
	if cur.Version != 2 {
		t.Fatalf("registry not bumped to v2, got v%d", cur.Version)
	}
	if len(reg.History("code-review")) != 2 {
		t.Fatal("previous version not retained (reversibility violated)")
	}
	// Reversible in one command.
	if _, err := reg.Rollback("code-review"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	cur, _ = reg.Current("code-review")
	if cur.Prompt != baseVersion().Prompt {
		t.Fatalf("rollback did not restore previous prompt")
	}
}

// TestL1Promote_OrgIsProposalOnly: a clean candidate on a non-personal
// profile is proposal-only (Tier H) — never auto-applied.
func TestL1Promote_OrgIsProposalOnly(t *testing.T) {
	Unfreeze()
	reg := NewSkillRegistry()
	reg.Register(baseVersion())
	proposed := baseVersion()
	proposed.Prompt = "review the diff carefully"

	p := L1Pipeline{Registry: reg, Suite: passingSuite(), Limits: DefaultChangeBudgetLimits()}
	out := p.Evaluate(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "org"}, L1Stages{ShadowPass: true, CanaryPass: true})
	if out.Applied {
		t.Fatal("org candidate must never auto-apply")
	}
	if !out.ProposalOnly || out.Record.Stage != StageProposed {
		t.Fatalf("expected proposal-only for org, got %+v", out)
	}
	if v, _ := reg.Current("code-review"); v.Version != 1 {
		t.Fatalf("org proposal wrongly bumped the registry to v%d", v.Version)
	}
}

// TestL1Quarantine_UntilShadowClean: a candidate is quarantined (not on the
// critical path) until shadow passes.
func TestL1Quarantine_UntilShadowClean(t *testing.T) {
	Unfreeze()
	reg := NewSkillRegistry()
	reg.Register(baseVersion())
	proposed := baseVersion()
	proposed.Prompt = "improved"
	p := L1Pipeline{Registry: reg, Suite: passingSuite(), Limits: DefaultChangeBudgetLimits()}
	out := p.Evaluate(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "personal"}, L1Stages{ShadowPass: false})
	if out.Applied || out.Record.Stage != StageQuarantined {
		t.Fatalf("expected quarantine while shadow unclean, got %+v", out)
	}
}

// TestL1PermissionExpandingRejected: the e2e's negative path at unit level —
// a permission-expanding candidate is rejected at the L1 gate, never promoted.
func TestL1PermissionExpandingRejected(t *testing.T) {
	Unfreeze()
	reg := NewSkillRegistry()
	reg.Register(baseVersion())
	proposed := baseVersion()
	proposed.Permissions = []string{"read", "write"}
	p := L1Pipeline{Registry: reg, Suite: passingSuite(), Limits: DefaultChangeBudgetLimits()}
	out := p.Evaluate(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "personal"}, L1Stages{ShadowPass: true, CanaryPass: true})
	if out.Applied || out.ConditionFailure != L1NewPermission {
		t.Fatalf("permission-expanding candidate must be rejected at the L1 gate, got %+v", out)
	}
}

// TestL1FrozenBlocksPromotion: when the drift budget is frozen, no promotion
// happens even for a clean personal candidate.
func TestL1FrozenBlocksPromotion(t *testing.T) {
	reg := NewSkillRegistry()
	reg.Register(baseVersion())
	proposed := baseVersion()
	proposed.Prompt = "improved"
	Freeze(FreezeQualityRegression)
	defer Unfreeze()
	p := L1Pipeline{Registry: reg, Suite: passingSuite(), Limits: DefaultChangeBudgetLimits()}
	out := p.Evaluate(L1Candidate{Base: baseVersion(), Proposed: proposed, Profile: "personal"}, L1Stages{ShadowPass: true, CanaryPass: true})
	if out.Applied {
		t.Fatalf("frozen budget must block promotion, got %+v", out)
	}
}

func TestSkillRegistryDefensivelyCopiesAuthoritySlices(t *testing.T) {
	registry := NewSkillRegistry()
	original := baseVersion()
	registry.Register(original)
	original.Permissions[0] = "write"
	original.DataClasses[0] = "secrets"

	current, ok := registry.Current(original.SkillID)
	if !ok {
		t.Fatal("registered skill is missing")
	}
	if current.Permissions[0] != "read" || current.DataClasses[0] != "code" {
		t.Fatalf("caller mutated registered authority: %+v", current)
	}
	current.Permissions[0] = "admin"
	current.DataClasses[0] = "credentials"
	history := registry.History(original.SkillID)
	if history[0].Permissions[0] != "read" || history[0].DataClasses[0] != "code" {
		t.Fatalf("returned current aliases registry authority: %+v", history[0])
	}
	history[0].Permissions[0] = "deploy"
	again, _ := registry.Current(original.SkillID)
	if again.Permissions[0] != "read" {
		t.Fatalf("returned history aliases registry authority: %+v", again)
	}
}
