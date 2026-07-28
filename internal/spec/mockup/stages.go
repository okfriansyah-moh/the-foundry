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
	reqs := make([]spec.Requirement, 0, len(items))
	high := make([]spec.Requirement, 0)
	normalized := make([]ExtractedItem, 0, len(items))
	for i, item := range items {
		item.Label = NormalizeLabel(item.Stage, item.Confidence, item.Label)
		normalized = append(normalized, item)
		req := spec.Requirement{
			ID:      fmt.Sprintf("mockup-%d", i+1),
			Section: item.Section,
			Text:    item.Text,
			Label:   item.Label,
			Basis:   string(item.Stage),
			Impact:  spec.ImpactMedium,
		}
		if HighImpactUnresolved(item.Text, item.Label) {
			req.Impact = spec.ImpactHigh
			high = append(high, req)
		}
		reqs = append(reqs, req)
	}
	return Extraction{
		Items:                normalized,
		HighImpactUnresolved: high,
		SeedRequirements:     reqs,
	}, nil
}
