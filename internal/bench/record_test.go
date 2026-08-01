package bench_test

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/bench"
)

func TestRunRecord_ValidateAndHumanInput(t *testing.T) {
	rec := bench.NewRunRecord("r1", bench.ArmControl, "wi1", "title", "digest")
	if err := rec.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := rec.ApplyHumanInput(bench.HumanInput{
		OrchestrationHours:   12.5,
		ManualPromptsTouches: 40,
	}); err != nil {
		t.Fatalf("ApplyHumanInput: %v", err)
	}
	o, ok := rec.ObservationFor(bench.MetricHumanOrchestration)
	if !ok || o.Value == nil || *o.Value != 12.5 {
		t.Fatalf("orchestration observation: %+v ok=%v", o, ok)
	}
	if o.Basis != bench.BasisHumanReported {
		t.Fatalf("basis = %s", o.Basis)
	}
}

func TestRunRecord_ApplyGitDelivery_ProxyBasis(t *testing.T) {
	rec := bench.NewRunRecord("r2", bench.ArmControl, "wi2", "title", "digest")
	g := bench.GitProvenance{
		MergeRef:      "abc",
		FirstCommitAt: parseTime(t, "2026-07-28T09:09:41+07:00"),
		MergedAt:      parseTime(t, "2026-07-28T12:11:38+07:00"),
		FilesChanged:  10,
	}
	if err := rec.ApplyGitDelivery(g, 2, "proxy note"); err != nil {
		t.Fatalf("ApplyGitDelivery: %v", err)
	}
	o, _ := rec.ObservationFor(bench.MetricPlanToFirstAccepted)
	if o.Basis != bench.BasisProxy {
		t.Fatalf("plan_to_first_accepted basis = %s, want proxy", o.Basis)
	}
	if !o.Proxy {
		t.Fatal("expected proxy flag")
	}
	def, _ := rec.ObservationFor(bench.MetricDefectsAfterHandoff)
	if def.Basis != bench.BasisProxy || !def.Proxy {
		t.Fatalf("defects observation: %+v", def)
	}
}

func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return ts.UTC()
}
