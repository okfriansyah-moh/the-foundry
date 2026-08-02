package intake

import (
	"context"
	"fmt"
)

// docs/PLAN.md Task 144 (INT-07): production intake adapter — wires real
// signal-backed validation and Temporal mission start without fixture flags.

// RealSignalCheck reports whether an opportunity has an allowlisted real
// validation signal. Kept local to avoid an intake→kernel→notify→intake cycle;
// cmd/foundry and foundryd inject kernel.StoreRealSignalVerifier.
type RealSignalCheck interface {
	HasAllowlistedRealSignal(ctx context.Context, opportunityID string) (bool, error)
}

// SignalBackedValidator wraps a Validator and refuses BUILD unless the
// OpportunityGate's real-signal verifier would accept the opportunity.
type SignalBackedValidator struct {
	Inner      Validator
	RealSignal RealSignalCheck
	// OpportunityIDForIdea maps an idea digest/run to an opportunity id used
	// for the real-signal lookup. When empty, BUILD is refused (fail-closed).
	OpportunityIDForIdea func(idea string) string
}

// Validate implements Validator with Task 146 signal gate semantics.
func (v SignalBackedValidator) Validate(ctx context.Context, in ValidateInput) (ValidateOutput, error) {
	if v.Inner == nil {
		return ValidateOutput{}, fmt.Errorf("intake: SignalBackedValidator requires Inner")
	}
	out, err := v.Inner.Validate(ctx, in)
	if err != nil {
		return ValidateOutput{}, err
	}
	if out.Verdict != "BUILD" {
		return out, nil
	}
	if v.RealSignal == nil {
		return ValidateOutput{
			Verdict:     "VALIDATE-MORE",
			Blockers:    []string{"real-validation-signal-wiring-absent"},
			NextActions: []string{"Configure StoreRealSignalVerifier before autonomous BUILD."},
			SpentUSD:    out.SpentUSD,
			Digest:      out.Digest,
		}, nil
	}
	oppID := ""
	if v.OpportunityIDForIdea != nil {
		oppID = v.OpportunityIDForIdea(in.Idea)
	}
	if oppID == "" {
		return ValidateOutput{
			Verdict:     "VALIDATE-MORE",
			Blockers:    []string{"missing-opportunity-id-for-real-signal"},
			NextActions: []string{"Bind the idea to an opportunity record before BUILD."},
			SpentUSD:    out.SpentUSD,
			Digest:      out.Digest,
		}, nil
	}
	ok, err := v.RealSignal.HasAllowlistedRealSignal(ctx, oppID)
	if err != nil {
		return ValidateOutput{}, fmt.Errorf("intake: real signal check: %w", err)
	}
	if !ok {
		return ValidateOutput{
			Verdict:     "VALIDATE-MORE",
			Blockers:    []string{"missing-allowlisted-real-validation-signal"},
			NextActions: []string{"Acquire an allowlisted real validation signal, then resume."},
			SpentUSD:    out.SpentUSD,
			Digest:      out.Digest,
		}, nil
	}
	return out, nil
}

// ProductionStarter starts PortfolioLoop/MissionLoop through Temporal.
type ProductionStarter struct {
	StartFn func(ctx context.Context, in StartMissionInput) (StartMissionOutput, error)
}

// Start implements MissionStarter.
func (p ProductionStarter) Start(ctx context.Context, in StartMissionInput) (StartMissionOutput, error) {
	if p.StartFn == nil {
		return StartMissionOutput{}, fmt.Errorf("intake: ProductionStarter.StartFn not configured")
	}
	return p.StartFn(ctx, in)
}
