package spec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// CandidateSource is the provider seam for requirement synthesis.
type CandidateSource interface {
	Synthesize(ctx context.Context, input string) ([]Requirement, error)
}

type Synthesizer struct {
	Source   CandidateSource
	Defaults Defaults
}

func (s Synthesizer) Synthesize(ctx context.Context, input string) (Specification, error) {
	if s.Source == nil {
		return Specification{}, fmt.Errorf("spec: synthesizer source is required")
	}
	reqs, err := s.Source.Synthesize(ctx, input)
	if err != nil {
		return Specification{}, fmt.Errorf("spec: synthesize: %w", err)
	}
	return PostPass(reqs, s.Defaults), nil
}

// ReplaySource serves deterministic, file-backed outputs for tests.
type ReplaySource struct {
	Requirements []Requirement `json:"requirements"`
}

func LoadReplaySource(path string) (ReplaySource, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReplaySource{}, fmt.Errorf("spec: read replay cassette %s: %w", path, err)
	}
	var rs ReplaySource
	if err := json.Unmarshal(raw, &rs); err != nil {
		return ReplaySource{}, fmt.Errorf("spec: decode replay cassette %s: %w", path, err)
	}
	return rs, nil
}

func (r ReplaySource) Synthesize(_ context.Context, _ string) ([]Requirement, error) {
	out := make([]Requirement, len(r.Requirements))
	copy(out, r.Requirements)
	return out, nil
}
