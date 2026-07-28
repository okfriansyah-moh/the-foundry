package mockup

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

func loadFixture(t *testing.T) FigmaFile {
	t.Helper()
	c, err := LoadFigmaCassette(filepath.Join("testdata", "figma", "checkout_flow.json"))
	if err != nil {
		t.Fatalf("LoadFigmaCassette: %v", err)
	}
	f, err := c.GetFile(context.Background(), "any-key")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	return f
}

// TestFigmaExtraction_SameShape proves Figma ingestion yields the same
// Extraction shape as the vision pipeline.
func TestFigmaExtraction_SameShape(t *testing.T) {
	ex := ExtractFigma(loadFixture(t))
	if len(ex.Items) == 0 || len(ex.SeedRequirements) == 0 {
		t.Fatalf("expected non-empty extraction, got %+v", ex)
	}
	if len(ex.Items) != len(ex.SeedRequirements) {
		t.Fatalf("items/requirements length mismatch: %d vs %d", len(ex.Items), len(ex.SeedRequirements))
	}
}

// TestFigmaObservedCarriesNodeRefBasis is the Task 80 acceptance:
// structurally-present facts are Observed and carry the Figma node ref as
// Basis.
func TestFigmaObservedCarriesNodeRefBasis(t *testing.T) {
	ex := ExtractFigma(loadFixture(t))
	observedWithNodeRef := 0
	for _, r := range ex.SeedRequirements {
		if r.Label == spec.LabelObserved {
			if !strings.HasPrefix(r.Basis, "figma:") {
				t.Fatalf("Observed requirement %q Basis %q does not carry a Figma node ref", r.ID, r.Basis)
			}
			observedWithNodeRef++
		}
	}
	if observedWithNodeRef == 0 {
		t.Fatal("expected at least one Observed requirement with a Figma node-ref Basis")
	}
}

// TestFigmaCapturesComponentsFlowsA11y proves the three structural facets are
// all extracted as Observed.
func TestFigmaCapturesComponentsFlowsA11y(t *testing.T) {
	ex := ExtractFigma(loadFixture(t))
	stages := map[Stage]bool{}
	for _, it := range ex.Items {
		if it.Label == spec.LabelObserved {
			stages[it.Stage] = true
		}
	}
	for _, want := range []Stage{StageScreenComponents, StageUserFlow, StageA11y} {
		if !stages[want] {
			t.Fatalf("expected Observed items for stage %q", want)
		}
	}
}

// TestFigmaNoInferenceStageObserved keeps the inference cap: nothing from a
// backend-inference stage is emitted by Figma structural ingestion.
func TestFigmaNoInferenceStageObserved(t *testing.T) {
	ex := ExtractFigma(loadFixture(t))
	for _, it := range ex.Items {
		if it.Stage == StageBackendInference {
			t.Fatalf("Figma ingestion must not emit backend-inference items: %+v", it)
		}
	}
}
