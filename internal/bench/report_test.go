package bench_test

import (
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/bench"
)

func TestCompare_BaselineOnlyInsufficientForFoundry(t *testing.T) {
	control := []*bench.RunRecord{
		mustRecord(t, "c1", 10, 5),
		mustRecord(t, "c2", 12, 6),
		mustRecord(t, "c3", 8, 4),
	}
	report := bench.Compare(control, nil)
	if report.Overall != bench.VerdictBaselineOnly {
		t.Fatalf("overall = %s", report.Overall)
	}
	var found bool
	for _, c := range report.Cells {
		if c.MetricID == bench.MetricHumanOrchestration && c.Verdict == bench.VerdictBaselineOnly {
			found = true
		}
		if c.MetricID == bench.MetricUnattendedRuntime && c.Verdict != bench.VerdictInsufficientData && c.Verdict != bench.VerdictBaselineOnly {
			t.Fatalf("unattended runtime verdict = %s", c.Verdict)
		}
	}
	if !found {
		t.Fatal("expected baseline-only verdict for orchestration metric")
	}
}

func TestCompare_InsufficientDataWhenOneArmMissingMetric(t *testing.T) {
	control := []*bench.RunRecord{mustRecord(t, "c1", 10, 5)}
	foundry := []*bench.RunRecord{bench.NewRunRecord("f1", bench.ArmFoundry, "f1", "f", "d")}
	report := bench.Compare(control, foundry)
	var orch bench.Cell
	for _, c := range report.Cells {
		if c.MetricID == bench.MetricHumanOrchestration {
			orch = c
		}
	}
	if orch.Verdict != bench.VerdictInsufficientData {
		t.Fatalf("orchestration verdict = %s, want insufficient data", orch.Verdict)
	}
}

func TestRenderMarkdown_IncludesTargetsLabel(t *testing.T) {
	targets, err := bench.LoadTargets("../../config/benchmark-targets.yaml")
	if err != nil {
		t.Fatal(err)
	}
	report := bench.Compare(nil, nil)
	md := bench.RenderMarkdown(report, targets)
	if !strings.Contains(md, "V1 acceptance targets") {
		t.Fatal("missing targets section")
	}
	if !strings.Contains(md, "not universal claims") {
		t.Fatal("missing disclaimer")
	}
}

func mustRecord(t *testing.T, id string, hours, touches float64) *bench.RunRecord {
	t.Helper()
	r := bench.NewRunRecord(id, bench.ArmControl, id, id, "digest")
	if err := r.ApplyHumanInput(bench.HumanInput{OrchestrationHours: hours, ManualPromptsTouches: int(touches)}); err != nil {
		t.Fatal(err)
	}
	return r
}
