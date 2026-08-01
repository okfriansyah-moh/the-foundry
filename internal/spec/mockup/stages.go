package mockup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

type ExtractedItem struct {
	Stage      Stage      `json:"stage"`
	Text       string     `json:"text"`
	Section    string     `json:"section"`
	Label      spec.Label `json:"label"`
	Confidence float64    `json:"confidence"`
	// NodeRef, when set, is the source reference for a structurally-present
	// fact (docs/PLAN.md Task 80 / EVO-07) — e.g. a Figma node/component/edge
	// ref. It becomes the requirement's Basis so a Figma-sourced Observed
	// item points back at the exact node it came from. Empty for
	// vision-extracted items (their Basis is the stage).
	NodeRef string `json:"node_ref,omitempty"`
}

type Extraction struct {
	Items                []ExtractedItem    `json:"items"`
	HighImpactUnresolved []spec.Requirement `json:"high_impact_unresolved"`
	SeedRequirements     []spec.Requirement `json:"seed_requirements"`
}

type VisionExtractor interface {
	Extract(ctx context.Context, artifact Artifact) ([]ExtractedItem, error)
}

type ReplayExtractor struct {
	Items []ExtractedItem `json:"items"`
}

func LoadReplayExtractor(path string) (ReplayExtractor, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReplayExtractor{}, fmt.Errorf("mockup extract: read cassette %s: %w", path, err)
	}
	var r ReplayExtractor
	if err := json.Unmarshal(raw, &r); err != nil {
		return ReplayExtractor{}, fmt.Errorf("mockup extract: decode cassette %s: %w", path, err)
	}
	return r, nil
}

func (r ReplayExtractor) Extract(_ context.Context, _ Artifact) ([]ExtractedItem, error) {
	out := make([]ExtractedItem, len(r.Items))
	copy(out, r.Items)
	return out, nil
}

func RunPipeline(ctx context.Context, ex VisionExtractor, artifact Artifact) (Extraction, error) {
	items, err := ex.Extract(ctx, artifact)
	if err != nil {
		return Extraction{}, err
	}
	return BuildExtraction("mockup", items), nil
}
