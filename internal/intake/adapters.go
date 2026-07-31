package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// This file wires the pipeline seams to the real Foundry packages. The
// deterministic stages (spec synthesis via a CandidateSource, PLAN generation,
// admission classification) call the genuine logic; the network-bearing or
// authority-bearing seams (opportunity validation, approval, mission start,
// readiness) are supplied by the caller so that tests inject cassettes/fakes
// and production injects the live services.

// OpportunityResolver turns an idea into a fully-evaluable Opportunity: its ICP,
// evidence claims and economic envelope. Offline tests load a fixture; a live
// deployment runs the research intake path (Task 101/109).
type OpportunityResolver interface {
	Resolve(ctx context.Context, idea string) (opportunity.Opportunity, error)
}

// OpportunityValidatorAdapter runs the genuine Score/Decide verdict logic over a
// resolved Opportunity. It is the real stage-2 validator.
type OpportunityValidatorAdapter struct {
	Config   opportunity.Config
	Resolver OpportunityResolver
}

// Validate implements Validator.
func (v OpportunityValidatorAdapter) Validate(ctx context.Context, in ValidateInput) (ValidateOutput, error) {
	opp, err := v.Resolver.Resolve(ctx, in.Idea)
	if err != nil {
		return ValidateOutput{}, fmt.Errorf("intake: resolve opportunity: %w", err)
	}
	sc := opportunity.Score(opp, v.Config)
	verdict, blockers := opportunity.Decide(sc, v.Config.Thresholds)
	spent := opp.EstimatedValidationCostUSD
	if in.ResearchCapUSD > 0 && spent > in.ResearchCapUSD {
		spent = in.ResearchCapUSD
	}
	return ValidateOutput{
		Verdict:     string(verdict),
		Blockers:    blockers,
		NextActions: nextActionsFor(verdict, blockers),
		SpentUSD:    spent,
		Digest:      digest(in.Idea),
	}, nil
}

// nextActionsFor returns the operator's next actions for a terminal-by-design
// verdict. A BUILD has none (the pipeline continues).
func nextActionsFor(v opportunity.Verdict, blockers []string) []string {
	switch v {
	case opportunity.VerdictReject:
		out := []string{"This opportunity was rejected; no repository, plan or build budget was created."}
		if len(blockers) > 0 {
			out = append(out, "Reasons: "+strings.Join(blockers, ", "))
		}
		out = append(out, "Gather stronger evidence or reframe the idea, then submit a new run.")
		return out
	case opportunity.VerdictValidateMore:
		out := []string{"This opportunity needs more validation before a build is justified."}
		if len(blockers) > 0 {
			out = append(out, "Unmet thresholds: "+strings.Join(blockers, ", "))
		}
		out = append(out, "Collect the missing evidence, then resume the run.")
		return out
	default:
		return nil
	}
}

// SpecSynthesizerAdapter wraps a spec.Synthesizer (whose Source is a cassette in
// tests or an LLM CandidateSource in production).
type SpecSynthesizerAdapter struct {
	Synth spec.Synthesizer
}

// Synthesize implements SpecSynthesizer.
func (a SpecSynthesizerAdapter) Synthesize(ctx context.Context, in SynthInput) (SynthOutput, error) {
	specDoc, err := a.Synth.Synthesize(ctx, in.Idea)
	if err != nil {
		return SynthOutput{}, err
	}
	b, err := json.Marshal(specDoc)
	if err != nil {
		return SynthOutput{}, fmt.Errorf("intake: marshal specification: %w", err)
	}
	return SynthOutput{SpecJSON: b, Digest: spec.InputDigest(in.Idea)}, nil
}

// PlanGeneratorAdapter wraps spec.PlanFromSpecification with the mission's real
// repository/effect context. It never self-classifies (C6).
type PlanGeneratorAdapter struct {
	Mapping spec.EffectMapping
	Mission spec.MissionContext
}

// Generate implements PlanGenerator. The specification's Sections/BySection are
// derived (json:"-"), so they are rebuilt from the round-tripped requirements
// via spec.PostPass before generation.
func (a PlanGeneratorAdapter) Generate(_ context.Context, in PlanGenInput) (PlanGenOutput, error) {
	var carried struct {
		Requirements []spec.Requirement `json:"requirements"`
	}
	if err := json.Unmarshal(in.SpecJSON, &carried); err != nil {
		return PlanGenOutput{}, fmt.Errorf("intake: decode specification: %w", err)
	}
	s := spec.PostPass(carried.Requirements, spec.Defaults{})
	mc := a.Mission
	mc.BudgetUSD = in.EnvelopeUSD
	b, err := spec.PlanFromSpecification(in.PlanID, in.Title, s, a.Mapping, mc)
	if err != nil {
		return PlanGenOutput{}, fmt.Errorf("intake: generate plan: %w", err)
	}
	return PlanGenOutput{PlanBytes: b, PlanID: in.PlanID}, nil
}

// AdmitterAdapter wraps plan parsing + admission.Classify. It reads the tier and
// reports whether strong-auth is required (TierH) without deciding anything.
type AdmitterAdapter struct {
	Policy admission.PolicyView
}

// Classify implements Admitter.
func (a AdmitterAdapter) Classify(_ context.Context, in AdmitInput) (AdmitOutput, error) {
	doc, err := plan.ParseBytes(in.PlanBytes)
	if err != nil {
		return AdmitOutput{}, fmt.Errorf("intake: parse generated plan: %w", err)
	}
	dec, err := admission.Classify(doc, a.Policy)
	if err != nil {
		// A self-classified plan is a hard gate; a generated plan never sets a
		// tier, so this is a programmer error worth surfacing.
		return AdmitOutput{}, fmt.Errorf("intake: classify generated plan: %w", err)
	}
	return AdmitOutput{
		Tier:               dec.Tier.String(),
		PolicyDigest:       dec.PolicyDigest,
		RequiresStrongAuth: dec.Tier == admission.TierH,
	}, nil
}
