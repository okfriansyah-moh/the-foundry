package kernel

import "testing"

func TestValidateTenXOrchestrationInput_RejectsChangeSets(t *testing.T) {
	err := ValidateTenXOrchestrationInput(TenXOrchestrationInput{
		ApprovedPlanID: "ap1", PlanDigest: "pd", EnvelopeDigest: "ed",
		UntrustedChangeSets: []string{"cs-1"},
	})
	if err == nil {
		t.Fatal("expected refusal of caller change sets")
	}
}

func TestValidateTenXOrchestrationInput_RequiresEnvelope(t *testing.T) {
	err := ValidateTenXOrchestrationInput(TenXOrchestrationInput{
		ApprovedPlanID: "ap1", PlanDigest: "pd",
	})
	if err == nil {
		t.Fatal("expected missing envelope refusal")
	}
}

func TestDeriveTenXOrchestrationPlan(t *testing.T) {
	p, err := DeriveTenXOrchestrationPlan("run-1", []string{"t1", "t2"}, "wave", "man")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AtomicGroupIDs) != 2 || p.ManifestDigest != "man" {
		t.Fatalf("%+v", p)
	}
}
