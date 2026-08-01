package mockup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// CassetteVisionExtractor is a VisionExtractor backed by per-input cassettes.
// Labels are capped at Inferred — vision output is never Observed.
type CassetteVisionExtractor struct {
	Dir string
}

// InputDigest returns a stable hex sha256 digest for artifact content.
func InputDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (c CassetteVisionExtractor) cassettePath(content []byte, basename string) (string, error) {
	if c.Dir == "" {
		return "", fmt.Errorf("mockup vision: cassette directory is required")
	}
	stem := strings.TrimSuffix(basename, filepath.Ext(basename))
	candidates := []string{
		filepath.Join(c.Dir, stem+".json"),
		filepath.Join(c.Dir, "vision_"+InputDigest(content)+".json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("mockup vision: no cassette for input %q (tried %v)", basename, candidates)
}

func (c CassetteVisionExtractor) Extract(ctx context.Context, artifact Artifact) ([]ExtractedItem, error) {
	_ = ctx
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		return nil, fmt.Errorf("mockup vision: read %s: %w", artifact.Path, err)
	}
	path, err := c.cassettePath(raw, artifact.Name)
	if err != nil {
		return nil, err
	}
	replay, err := LoadReplayExtractor(path)
	if err != nil {
		return nil, err
	}
	items, err := replay.Extract(ctx, artifact)
	if err != nil {
		return nil, err
	}
	return capVisionLabels(items), nil
}

func capVisionLabels(items []ExtractedItem) []ExtractedItem {
	out := make([]ExtractedItem, len(items))
	for i, item := range items {
		item.Label = NormalizeLabel(item.Stage, item.Confidence, item.Label)
		if item.Label == spec.LabelObserved {
			item.Label = spec.LabelInferred
		}
		item.NodeRef = ""
		out[i] = item
	}
	return out
}

func extractVision(ctx context.Context, artifact Artifact, vision CassetteVisionExtractor) (Extraction, error) {
	items, err := vision.Extract(ctx, artifact)
	if err != nil {
		return Extraction{}, err
	}
	return BuildExtraction("mockup", items), nil
}

// LoadVisionCassette loads a vision replay cassette from path.
func LoadVisionCassette(path string) (ReplayExtractor, error) {
	return LoadReplayExtractor(path)
}

// WriteVisionCassette records items for deterministic replay (test helper).
func WriteVisionCassette(path string, items []ExtractedItem) error {
	raw, err := json.MarshalIndent(ReplayExtractor{Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
