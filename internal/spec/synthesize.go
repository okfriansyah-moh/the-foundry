package spec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	spec := PostPass(reqs, s.Defaults)
	// A source that carries provenance (an LLM synthesis call) records what
	// produced this specification, so a spec can be traced to what produced it
	// (provenance, not authorization — Task 109 / C16).
	if p, ok := s.Source.(ProvenanceSource); ok {
		spec.Provenance = p.Provenance()
	}
	return spec, nil
}

// ProvenanceSource is an optional CandidateSource capability: a source that can
// report what produced its requirements (provider/model/prompt digest).
type ProvenanceSource interface {
	Provenance() SpecProvenance
}

// ReplaySource serves deterministic, file-backed outputs for tests.
type ReplaySource struct {
	// InputDigest, when non-empty, keys this cassette to a specific input: a
	// replay whose input does not hash to this digest fails loudly instead of
	// silently returning someone else's requirements (docs/PLAN.md Task 109).
	// Empty preserves the legacy input-agnostic replay behavior.
	InputDigest  string        `json:"input_digest,omitempty"`
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

// InputDigest returns the hex sha256 of a synthesis input, the key a
// digest-keyed cassette is matched against.
func InputDigest(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (r ReplaySource) Synthesize(_ context.Context, input string) ([]Requirement, error) {
	if r.InputDigest != "" {
		if got := InputDigest(input); got != r.InputDigest {
			return nil, fmt.Errorf("spec: replay cassette input mismatch: input digest %s does not match cassette key %s", got, r.InputDigest)
		}
	}
	out := make([]Requirement, len(r.Requirements))
	copy(out, r.Requirements)
	return out, nil
}
