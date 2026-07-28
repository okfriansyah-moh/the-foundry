package mission

import "testing"

func twoMissionPortfolio(t *testing.T) *Portfolio {
	t.Helper()
	p := NewPortfolio(2)
	if err := p.AddMission(PortfolioMission{ID: "alpha", MonthlyBudgetUSD: 100, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.AddMission(PortfolioMission{ID: "beta", MonthlyBudgetUSD: 100, Active: true, RevenueBearing: true}); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPortfolioBudgetIsolation proves budget bleed between missions is
// impossible: exhausting alpha never affects beta, and a charge only ever
// touches its own mission's envelope (ledger-style proof).
func TestPortfolioBudgetIsolation(t *testing.T) {
	p := twoMissionPortfolio(t)

	// Exhaust alpha completely.
	if err := p.Charge("alpha", 100); err != nil {
		t.Fatalf("charge alpha: %v", err)
	}
	// A further alpha charge fails closed on alpha's own envelope.
	if err := p.Charge("alpha", 1); err == nil {
		t.Fatal("alpha over-budget charge should fail closed")
	}
	// beta is completely unaffected — no bleed.
	if got := p.Remaining("beta"); got != 100 {
		t.Fatalf("beta remaining = %.2f, want 100 (budget bled across missions)", got)
	}
	if err := p.Charge("beta", 100); err != nil {
		t.Fatalf("beta should still have its full envelope: %v", err)
	}
}

// TestPortfolioFairness_StarvingAttempt proves one mission cannot starve the
// other: even when the caller keeps asking for work, the fair scheduler keeps
// the two within one turn of each other, so beta is scheduled ~half the time.
func TestPortfolioFairness_StarvingAttempt(t *testing.T) {
	p := twoMissionPortfolio(t)
	seq := p.Schedule(10)
	counts := map[string]int{}
	for _, id := range seq {
		counts[id]++
	}
	if counts["alpha"] != 5 || counts["beta"] != 5 {
		t.Fatalf("fair scheduler should split 10 rounds 5/5, got %v", counts)
	}
	if spread := p.FairnessSpread(); spread > 1 {
		t.Fatalf("fairness spread = %d, want <= 1 (a mission is starving)", spread)
	}
}

// TestPortfolioMaxActiveProducts enforces the maximum_active_products cap.
func TestPortfolioMaxActiveProducts(t *testing.T) {
	p := NewPortfolio(1)
	if err := p.AddMission(PortfolioMission{ID: "a", Active: true}); err != nil {
		t.Fatal(err)
	}
	// Adding a second ACTIVE mission over the cap must fail closed.
	if err := p.AddMission(PortfolioMission{ID: "b", Active: true}); err == nil {
		t.Fatal("adding a second active mission should exceed max_active_products")
	}
	// Adding it idle is fine; activating it is blocked until room frees up.
	if err := p.AddMission(PortfolioMission{ID: "b", Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate("b"); err == nil {
		t.Fatal("activating b over the cap should fail closed")
	}
	if err := p.Deactivate("a"); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate("b"); err != nil {
		t.Fatalf("activating b after freeing a slot should succeed: %v", err)
	}
}

// TestPortfolioKillRevenueBearingRequiresHuman proves a kill-candidate for a
// revenue-bearing product is forced to human approval (Tier H).
func TestPortfolioKillRevenueBearingRequiresHuman(t *testing.T) {
	p := twoMissionPortfolio(t)
	// beta is revenue-bearing.
	d, err := p.ProposeDecision("beta", DecisionKillCandidate, "underperforming")
	if err != nil {
		t.Fatal(err)
	}
	if !d.RequiresHumanApproval {
		t.Fatal("killing a revenue-bearing product must require human approval")
	}
	// alpha is not revenue-bearing: a hold proposal needs no human gate.
	d2, _ := p.ProposeDecision("alpha", DecisionHold, "steady")
	if d2.RequiresHumanApproval {
		t.Fatal("a hold on a non-revenue mission should not require human approval")
	}
}

func TestPortfolioDigestRenders(t *testing.T) {
	p := twoMissionPortfolio(t)
	_ = p.Charge("alpha", 25)
	p.Schedule(3)
	out := FormatPortfolioDigest(p)
	for _, want := range []string{"Portfolio", "alpha", "beta", "spent $25.00"} {
		if !contains(out, want) {
			t.Fatalf("digest missing %q:\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
