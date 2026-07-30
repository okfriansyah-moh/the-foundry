package spec_test

import (
	"context"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// fakeProvider echoes a fixed requirement set, honoring whatever label the
// caller wants to test the cap against.
type fakeProvider struct {
	reqs        []spec.Requirement
	confidences []float64
	sawPrompt   string
}

func (f *fakeProvider) Propose(_ context.Context, prompt string) (spec.ProposedRequirements, error) {
	f.sawPrompt = prompt
	return spec.ProposedRequirements{Requirements: f.reqs, Confidences: f.confidences, Provider: "fake", Model: "fake-1"}, nil
}

func TestLLMCandidateSourceCapsAtInferred(t *testing.T) {
	fp := &fakeProvider{reqs: []spec.Requirement{
		{ID: "r1", Section: "auth", Text: "login works", Label: spec.LabelObserved, Basis: "model says so"},
		{ID: "r2", Section: "billing", Text: "charge works", Label: spec.LabelObserved},
		{ID: "r3", Section: "apis", Text: "api exists", Label: spec.Label("bogus")},
	}}
	src := &spec.LLMCandidateSource{Provider: fp}
	reqs, err := src.Synthesize(context.Background(), "an idea")
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	for _, r := range reqs {
		if r.Label == spec.LabelObserved {
			t.Fatalf("synthesis output must never be Observed: %+v", r)
		}
	}
	// Invalid label failed closed to Unresolved.
	found := false
	for _, r := range reqs {
		if r.ID == "r3" && r.Label == spec.LabelUnresolved {
			found = true
		}
	}
	if !found {
		t.Fatal("invalid label must fail closed to Unresolved")
	}
	// Input is delimited as untrusted data in the prompt (containment).
	if !strings.Contains(fp.sawPrompt, "UNTRUSTED DATA") {
		t.Fatalf("prompt must fence the input as untrusted data: %q", fp.sawPrompt)
	}
	// Provenance recorded.
	prov := src.Provenance()
	if prov.Provider != "fake" || prov.Model != "fake-1" || prov.PromptDigest == "" {
		t.Fatalf("provenance not recorded: %+v", prov)
	}
}

func TestLLMSourceThroughPostPassNeverObservedWithoutBasis(t *testing.T) {
	// Property-style: for a spread of model outputs, no requirement ever ends
	// Observed after PostPass (synthesis caps them all at Inferred).
	labels := []spec.Label{spec.LabelObserved, spec.LabelInferred, spec.LabelAssumed, spec.LabelUnresolved, spec.Label("x")}
	var reqs []spec.Requirement
	for i, l := range labels {
		reqs = append(reqs, spec.Requirement{ID: "r", Section: "auth", Text: "t", Label: l, Basis: ""})
		_ = i
	}
	src := &spec.LLMCandidateSource{Provider: &fakeProvider{reqs: reqs}}
	syn := spec.Synthesizer{Source: src}
	out, err := syn.Synthesize(context.Background(), "idea")
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	for _, r := range out.Requirements {
		if r.Label == spec.LabelObserved {
			t.Fatalf("no requirement may be Observed after LLM synthesis: %+v", r)
		}
	}
	if out.Provenance.PromptDigest == "" {
		t.Fatal("specification must carry synthesis provenance")
	}
}

func TestReplaySourceInputKeyingFailsLoudly(t *testing.T) {
	rs := spec.ReplaySource{
		InputDigest:  spec.InputDigest("the-right-input"),
		Requirements: []spec.Requirement{{ID: "r1", Section: "auth", Text: "t", Label: spec.LabelInferred}},
	}
	if _, err := rs.Synthesize(context.Background(), "a-different-input"); err == nil {
		t.Fatal("a replay whose input does not match its cassette key must fail loudly")
	}
	if _, err := rs.Synthesize(context.Background(), "the-right-input"); err != nil {
		t.Fatalf("matching input must replay cleanly: %v", err)
	}
}

func TestKeyedCassetteReplaysForItsInput(t *testing.T) {
	const idea = "Build a SaaS for scheduling client calls for solo consultants"
	rs, err := spec.LoadReplaySource("../../test/cassettes/spec/idea-scheduling.json")
	if err != nil {
		t.Fatalf("load cassette: %v", err)
	}
	if _, err := rs.Synthesize(context.Background(), idea); err != nil {
		t.Fatalf("cassette must replay for its keyed input: %v", err)
	}
	if _, err := rs.Synthesize(context.Background(), "some other idea"); err == nil {
		t.Fatal("cassette must refuse a non-matching input")
	}
}
