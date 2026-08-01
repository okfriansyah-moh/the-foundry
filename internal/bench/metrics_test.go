package bench_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/bench"
)

func TestAllMetrics_CountAndUnits(t *testing.T) {
	metrics := bench.AllMetrics()
	if len(metrics) != 15 {
		t.Fatalf("got %d metrics, want 15", len(metrics))
	}
	seen := make(map[bench.MetricID]struct{})
	for _, m := range metrics {
		if m.ObservationPoint == "" {
			t.Errorf("metric %s missing observation point", m.ID)
		}
		if m.NotMeasurableRule == "" {
			t.Errorf("metric %s missing not-measurable rule", m.ID)
		}
		if _, ok := seen[m.ID]; ok {
			t.Errorf("duplicate metric id %s", m.ID)
		}
		seen[m.ID] = struct{}{}
	}
}

func TestLoadTargets(t *testing.T) {
	targets, err := bench.LoadTargets("../../config/benchmark-targets.yaml")
	if err != nil {
		t.Fatalf("LoadTargets: %v", err)
	}
	if targets.Label != "V1 acceptance targets" {
		t.Fatalf("label = %q", targets.Label)
	}
	if targets.Personal.ManualOrchestrationReduction != 0.5 {
		t.Fatalf("personal orchestration reduction = %v", targets.Personal.ManualOrchestrationReduction)
	}
}
