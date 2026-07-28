package mockup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

func TestNormalizeLabel_InferenceStageNeverObserved(t *testing.T) {
	got := NormalizeLabel(StageBackendInference, 0.99, spec.LabelObserved)
	if got == spec.LabelObserved {
		t.Fatal("backend inference stage emitted Observed label, which is forbidden")
	}
}

func TestRunPipeline_FixturesAuthBillingNotObserved(t *testing.T) {
	cases := []string{
		"../../../test/cassettes/mockup/landing_form.json",
		"../../../test/cassettes/mockup/three_screen_app.json",
	}
	for _, cassette := range cases {
		t.Run(cassette, func(t *testing.T) {
			replay, err := LoadReplayExtractor(cassette)
			if err != nil {
				t.Fatalf("LoadReplayExtractor: %v", err)
			}
			artifact, err := Ingest("fixture.pdf", "application/pdf", []byte("fixture"), time.Now().UTC())
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			out, err := RunPipeline(context.Background(), replay, artifact)
			if err != nil {
				t.Fatalf("RunPipeline: %v", err)
			}
			for _, req := range out.SeedRequirements {
				txt := strings.ToLower(req.Text)
				isSensitive := strings.Contains(txt, "auth") || strings.Contains(txt, "billing") || strings.Contains(txt, "payment")
				if !isSensitive {
					continue
				}
				if req.Label == spec.LabelObserved {
					t.Fatalf("sensitive requirement labeled Observed: %+v", req)
				}
				if req.Label != spec.LabelAssumed && req.Label != spec.LabelUnresolved && req.Label != spec.LabelInferred {
					t.Fatalf("unexpected label for sensitive requirement: %+v", req)
				}
			}
		})
	}
}
