package evolve

import "testing"

func TestL0Pipeline_Evaluate(t *testing.T) {
	pipe := L0Pipeline{Registry: TunableRegistry{Tunables: []Tunable{{Name: "batch_size", Min: 1, Max: 10}}}}
	record, err := pipe.Evaluate(Candidate{Name: "batch_size", Current: 3, Proposed: 5}, Evaluation{ReplayPass: true, ShadowPass: true, CanaryPass: true})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if record.Stage != StagePromoted {
		t.Fatalf("stage=%s want %s", record.Stage, StagePromoted)
	}
}
