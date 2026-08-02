package kernel

import "testing"

func TestTenXOrchestration_ReplayDeterminism(t *testing.T) {
	a, err := DeriveTenXOrchestrationPlan("run-1", []string{"t1", "t2"}, "wave", "man")
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeriveTenXOrchestrationPlan("run-1", []string{"t1", "t2"}, "wave", "man")
	if err != nil {
		t.Fatal(err)
	}
	if a.ManifestDigest != b.ManifestDigest || len(a.AtomicGroupIDs) != len(b.AtomicGroupIDs) {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}
