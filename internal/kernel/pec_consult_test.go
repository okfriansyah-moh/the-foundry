package kernel_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/pec"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// TestPECConsult_MalformedProposalDistrust verifies the kernel's distrust behavior:
// a WaveProposal with an unknown task ID is rejected by ValidateWaveProposal,
// which means DeliverPlan falls back to sequential ordering.
func TestPECConsult_MalformedProposalDistrust(t *testing.T) {
	doc := plan.Document{
		Tasks: []plan.Task{
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "a"},
		},
	}
	// Malformed: references a task not in the plan.
	malformed := pec.WaveProposal{
		Waves: [][]string{{"ghost"}},
	}
	if err := pec.ValidateWaveProposal(malformed, doc); err == nil {
		t.Error("ValidateWaveProposal should reject unknown task ID")
	}

	// Valid proposal should be accepted.
	valid := pec.WaveProposal{
		Waves: [][]string{{"a"}, {"b"}},
	}
	if err := pec.ValidateWaveProposal(valid, doc); err != nil {
		t.Errorf("ValidateWaveProposal rejected valid proposal: %v", err)
	}
}

// TestPECConsult_DependencyHonestOrdering verifies that PEC produces
// dependency-honest waves that the kernel uses for execution order.
func TestPECConsult_DependencyHonestOrdering(t *testing.T) {
	doc := plan.Document{
		Tasks: []plan.Task{
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "a"},
		},
	}
	proposal, err := pec.ProposeWaves(doc)
	if err != nil {
		t.Fatalf("ProposeWaves: %v", err)
	}
	if len(proposal.Waves) < 2 {
		t.Fatalf("expected >= 2 waves for a→b chain, got %d", len(proposal.Waves))
	}
	// Wave 0 must contain "a", wave 1 must contain "b".
	found := func(wave []string, id string) bool {
		for _, v := range wave {
			if v == id {
				return true
			}
		}
		return false
	}
	if !found(proposal.Waves[0], "a") {
		t.Errorf("wave[0] does not contain 'a': %v", proposal.Waves[0])
	}
	if !found(proposal.Waves[1], "b") {
		t.Errorf("wave[1] does not contain 'b': %v", proposal.Waves[1])
	}
}
