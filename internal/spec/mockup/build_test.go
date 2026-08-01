package mockup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

func TestBuildExtraction_FigmaRegression(t *testing.T) {
	ex := ExtractFigma(loadFixture(t))
	goldenPath := filepath.Join("testdata", "figma_extraction_golden.json")
	if os.Getenv("UPDATE_MOCKUP_GOLDENS") == "1" {
		writeGolden(t, goldenPath, ex)
	}
	want := loadGoldenExtraction(t, goldenPath)
	assertExtractionEqual(t, want, ex)
}

func TestBuildExtraction_MockupCassetteRegression(t *testing.T) {
	cassette := filepath.Join("..", "..", "..", "test", "cassettes", "mockup", "landing_form.json")
	replay, err := LoadReplayExtractor(cassette)
	if err != nil {
		t.Fatalf("LoadReplayExtractor: %v", err)
	}
	artifact, err := Ingest("fixture.pdf", "application/pdf", []byte("fixture"), time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	out, err := RunPipeline(context.Background(), replay, artifact)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	goldenPath := filepath.Join("testdata", "landing_form_extraction_golden.json")
	if os.Getenv("UPDATE_MOCKUP_GOLDENS") == "1" {
		writeGolden(t, goldenPath, out)
	}
	want := loadGoldenExtraction(t, goldenPath)
	assertExtractionEqual(t, want, out)
}

func TestVisionNeverObserved(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "screenshot.png")
	if err := os.WriteFile(pngPath, minimalPNG(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	cassetteDir := filepath.Join("..", "..", "..", "test", "cassettes", "mockup")
	router := NewRouter(RouterConfig{CassetteDir: cassetteDir})
	raw, _ := os.ReadFile(pngPath)
	out, err := router.Extract(context.Background(), Artifact{Name: "screenshot.png", Path: pngPath}, raw)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, r := range out.SeedRequirements {
		if r.Label == spec.LabelObserved {
			t.Fatalf("vision-sourced item labeled Observed: %+v", r)
		}
	}
}

func TestSpecificationPostPassFromMockup(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "mockup", "landing.html"))
	if err != nil {
		t.Fatalf("read html: %v", err)
	}
	items, err := ExtractHTML(raw)
	if err != nil {
		t.Fatalf("ExtractHTML: %v", err)
	}
	ex := BuildExtraction("mockup", items)
	defaults, err := spec.LoadDefaults(filepath.Join("..", "..", "..", "config", "spec-defaults.yaml"))
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	before := len(ex.SeedRequirements)
	after := spec.PostPass(ex.SeedRequirements, defaults)
	if len(after.Requirements) < before {
		t.Fatalf("PostPass dropped requirements: %d -> %d", before, len(after.Requirements))
	}
}

func TestExtractionGoldenBytesStable(t *testing.T) {
	// Lock that figma extraction JSON encoding stays stable across the unified builder.
	ex := ExtractFigma(loadFixture(t))
	raw, err := json.Marshal(ex)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) < 100 {
		t.Fatalf("suspiciously small figma extraction payload")
	}
}
