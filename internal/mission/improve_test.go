package mission

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// testAdmitter is a controllable ImprovementAdmitter for tests.
type testAdmitter struct {
	tier admission.Tier
	err  error
}

func (a *testAdmitter) Classify(_ *plan.Document) (admission.Decision, error) {
	return admission.Decision{Tier: a.tier}, a.err
}

func newDoc(id string) *plan.Document {
	return &plan.Document{
		ID:    id,
		Title: "test improvement",
		Tasks: []plan.Task{{ID: "t1", Goal: "copy tweak", Commands: []string{"make test"}}},
	}
}

// TestRunImproveCycle_InEnvelope verifies a copy-tweak at A0 auto-admits.
func TestRunImproveCycle_InEnvelope(t *testing.T) {
	ctx := context.Background()
	gen := &CassetteGenerator{Doc: newDoc("improve-mission1-1")}
	adm := &testAdmitter{tier: admission.TierA0}

	result, err := RunImproveCycle(ctx, ImproveCycleInput{
		MissionID:    "mission1",
		ProductID:    "prod1",
		Observation:  Observation{ActivationRate: 0.1, ConversionRate: 0.02, NetMRRUSD: 100, NoProgressCycles: 0},
		BudgetCapUSD: 50,
		AutoTiers:    []string{"A0", "A1"},
		Generator:    gen,
		Admitter:     adm,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Admitted {
		t.Errorf("Admitted=false, want true; HaltReason=%q", result.HaltReason)
	}
	if result.Tier != "A0" {
		t.Errorf("Tier=%q, want A0", result.Tier)
	}
	if result.Promotion == nil {
		t.Fatal("Promotion is nil, want non-nil for admitted cycle")
	}
	if result.Promotion.Level != "plan-cycle" {
		t.Errorf("Promotion.Level=%q, want plan-cycle", result.Promotion.Level)
	}
}

// TestRunImproveCycle_HaltAtH verifies that an H-tier plan halts pre-build.
func TestRunImproveCycle_HaltAtH(t *testing.T) {
	ctx := context.Background()
	gen := &CassetteGenerator{Doc: newDoc("improve-mission1-2")}
	adm := &testAdmitter{tier: admission.TierH}

	result, err := RunImproveCycle(ctx, ImproveCycleInput{
		MissionID:    "mission1",
		ProductID:    "prod1",
		Observation:  Observation{NoProgressCycles: 0},
		BudgetCapUSD: 50,
		AutoTiers:    []string{"A0", "A1"}, // H not in envelope
		Generator:    gen,
		Admitter:     adm,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Admitted {
		t.Error("Admitted=true, want false for H tier")
	}
	if result.Tier != "H" {
		t.Errorf("Tier=%q, want H", result.Tier)
	}
	if result.HaltReason == "" {
		t.Error("HaltReason is empty, want non-empty halt message")
	}
	if result.Promotion != nil {
		t.Error("Promotion non-nil, want nil when halted")
	}
}

// TestRunImproveCycle_NewDependencyRaisedTier verifies that a new-dependency
// plan (A2) above A0/A1 envelope also halts.
func TestRunImproveCycle_NewDependencyRaisedTier(t *testing.T) {
	ctx := context.Background()
	gen := &CassetteGenerator{Doc: newDoc("improve-mission1-3")}
	adm := &testAdmitter{tier: admission.TierA2}

	result, err := RunImproveCycle(ctx, ImproveCycleInput{
		MissionID: "mission1",
		ProductID: "prod1",
		AutoTiers: []string{"A0", "A1"},
		Generator: gen,
		Admitter:  adm,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Admitted {
		t.Errorf("Admitted=true, want false for A2 above A0/A1 envelope")
	}
}

// TestPlanDocFromSpec verifies provenance ID encoding.
func TestPlanDocFromSpec(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := PlanDocFromSpec("m1", "prod1", "bump font size", now)
	if doc.ID == "" {
		t.Fatal("doc.ID is empty")
	}
	if len(doc.Tasks) != 1 {
		t.Errorf("len(Tasks)=%d, want 1", len(doc.Tasks))
	}
	// creator provenance is encoded in the ID prefix per Task 51 design note.
	if doc.ID[:8] != "improve-" {
		t.Errorf("ID prefix %q, want improve-", doc.ID[:8])
	}
}
