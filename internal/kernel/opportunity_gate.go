package kernel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// ActivityRequireBuildVerdict is the registered name of the opportunity
// verdict-gate activity (docs/PLAN.md Task 102 / OPP-03).
const ActivityRequireBuildVerdict = "RequireBuildVerdict"

// VerdictLoader loads a stored verdict and re-loads the append-only evidence
// it was produced from, so the gate can re-derive the scorecard rather than
// trust the stored one (Constitution C23: an opportunity may not classify
// itself). *opportunity.Store satisfies this interface.
type VerdictLoader interface {
	LatestVerdict(ctx context.Context, opportunityID string) (opportunity.VerdictRecord, error)
	LoadOpportunity(ctx context.Context, opportunityID string) (opportunity.Opportunity, error)
}

// ValidationReserver reserves the bounded Phase-C validation envelope for a
// VALIDATE-MORE outcome. It is an interface so the gate stays testable without
// a cost-ledger database.
type ValidationReserver interface {
	Reserve(ctx context.Context, amountUSD float64, meta any) (reservationID string, err error)
}

// RealSignalVerifier reports whether an opportunity has a provenance-backed,
// allowlisted real validation signal (docs/PLAN.md Task 139). Synthetic /
// test-mode / unallowlisted signals return false, so they can never satisfy
// must_have_real_validation_signal. Until Task 139 wires the real
// implementation, DenyRealSignal is the fail-closed default.
type RealSignalVerifier interface {
	HasAllowlistedRealSignal(ctx context.Context, opportunityID string) (bool, error)
}

// DenyRealSignal is the fail-closed default RealSignalVerifier: it never
// affirms a real signal, so a BUILD verdict cannot be honored until Task 139
// supplies a verifier that recognizes allowlisted, provenance-backed records.
type DenyRealSignal struct{}

// HasAllowlistedRealSignal always returns false (fail-closed).
func (DenyRealSignal) HasAllowlistedRealSignal(context.Context, string) (bool, error) {
	return false, nil
}

// OpportunityGate is the kernel-owned, deterministic decision that makes an
// opportunity verdict bite. No venture build proceeds unless a BUILD verdict
// exists, was produced by the deterministic scorer over stored evidence, is
// reproducible, has not expired, fits the mission envelope, and is backed by a
// real validation signal (Constitution C4/C23).
type OpportunityGate struct {
	Loader     VerdictLoader
	Config     opportunity.Config
	Reserver   ValidationReserver
	RealSignal RealSignalVerifier
	Now        func() time.Time
}

// RequireBuildVerdictInput names the opportunity and the mission's build
// envelope.
type RequireBuildVerdictInput struct {
	OpportunityID      string  `json:"opportunity_id"`
	MissionEnvelopeUSD float64 `json:"mission_envelope_usd"`
}

// RequireBuildVerdictOutput is the gate's decision. Allowed is true only for a
// reproducible, in-envelope, real-signal-backed BUILD. Every other outcome
// carries a terminal ResultCode and the WorkflowStatus it maps to.
type RequireBuildVerdictOutput struct {
	Allowed               bool    `json:"allowed"`
	Verdict               string  `json:"verdict"`
	ResultCode            string  `json:"result_code"`
	Reason                string  `json:"reason"`
	WorkflowStatus        string  `json:"workflow_status"`
	ReservedValidationUSD float64 `json:"reserved_validation_usd"`
	ReservationID         string  `json:"reservation_id"`
}

func (g *OpportunityGate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *OpportunityGate) realSignal() RealSignalVerifier {
	if g.RealSignal != nil {
		return g.RealSignal
	}
	return DenyRealSignal{}
}

func refuse(code state.ResultCode, reason string) RequireBuildVerdictOutput {
	status, _ := state.KnownResultCode(code)
	return RequireBuildVerdictOutput{
		Allowed:        false,
		ResultCode:     string(code),
		Reason:         reason,
		WorkflowStatus: string(status),
	}
}

// RequireBuildVerdict re-derives the scorecard from the opportunity's stored,
// append-only evidence and the recorded config, and fails closed on any
// disagreement, absence, expiry, envelope breach or missing real signal. It
// never trusts the stored scorecard, an LLM output, a PEC proposal or an
// operator assertion — only a reproducible deterministic scorecard.
//
// It is a Temporal activity: signature (ctx, in) (out, error). A returned
// error is reserved for infrastructure faults (the caller retries); every
// business refusal is a successful call returning a terminal ResultCode.
func (g *OpportunityGate) RequireBuildVerdict(ctx context.Context, in RequireBuildVerdictInput) (RequireBuildVerdictOutput, error) {
	if g.Loader == nil {
		return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: opportunity gate has no verdict loader")
	}

	rec, err := g.Loader.LatestVerdict(ctx, in.OpportunityID)
	if errors.Is(err, opportunity.ErrNotFound) {
		return refuse(state.ResultOpportunityVerdictMissing, "verdict-missing"), nil
	}
	if err != nil {
		return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: load verdict: %w", err)
	}

	opp, err := g.Loader.LoadOpportunity(ctx, in.OpportunityID)
	if errors.Is(err, opportunity.ErrNotFound) {
		return refuse(state.ResultOpportunityVerdictMissing, "opportunity-evidence-missing"), nil
	}
	if err != nil {
		return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: load opportunity: %w", err)
	}

	// A verdict must be re-derivable under the exact config version that
	// produced it; a config change since then makes it unreproducible.
	if g.Config.Version != rec.ConfigVersion {
		return refuse(state.ResultOpportunityVerdictUnreproducible, "config-version-drift"), nil
	}

	reScored := opportunity.Score(opp, g.Config)
	reDigest, err := reScored.Digest()
	if err != nil {
		return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: re-derive scorecard digest: %w", err)
	}
	if reDigest != rec.ScorecardDigest {
		return refuse(state.ResultOpportunityVerdictUnreproducible, "scorecard-digest-mismatch"), nil
	}
	reVerdict, _ := opportunity.Decide(reScored, g.Config.Thresholds)
	if reVerdict != rec.Verdict {
		return refuse(state.ResultOpportunityVerdictUnreproducible, "verdict-disagrees-on-rederivation"), nil
	}

	out := RequireBuildVerdictOutput{Verdict: string(rec.Verdict)}

	switch rec.Verdict {
	case opportunity.VerdictReject:
		// Build nothing is a successful decision when evidence is weak.
		out.ResultCode = string(state.ResultOpportunityRejected)
		out.Reason = "opportunity-rejected"
		out.WorkflowStatus = string(state.StatusSucceeded)
		return out, nil

	case opportunity.VerdictValidateMore:
		amt := opp.EstimatedValidationCostUSD
		if cap := g.Config.Thresholds.ValidationCostCapUSD; cap > 0 && amt > cap {
			amt = cap
		}
		if amt < 0 {
			amt = 0
		}
		if g.Reserver != nil && amt > 0 {
			id, rerr := g.Reserver.Reserve(ctx, amt, map[string]string{
				"opportunity_id": in.OpportunityID,
				"purpose":        "opportunity-validation",
			})
			if rerr != nil {
				return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: reserve validation envelope: %w", rerr)
			}
			out.ReservationID = id
		}
		out.ReservedValidationUSD = amt
		out.ResultCode = string(state.ResultOpportunityValidationRequired)
		out.Reason = "validation-required"
		out.WorkflowStatus = string(state.StatusSucceeded)
		return out, nil

	case opportunity.VerdictBuild:
		// Expiry: an expired BUILD is not a usable current verdict.
		if hours := g.Config.Thresholds.MaxVerdictAgeHours; hours > 0 {
			maxAge := time.Duration(hours) * time.Hour
			if g.now().Sub(rec.CreatedAt) > maxAge {
				return refuse(state.ResultOpportunityVerdictMissing, "verdict-expired"), nil
			}
		}
		// The verdict's own budget cap may not exceed the mission envelope.
		if in.MissionEnvelopeUSD > 0 && rec.Thresholds.MaximumMVPBudgetUSD > in.MissionEnvelopeUSD {
			return refuse(state.ResultOpportunityVerdictMissing, "mvp-budget-exceeds-mission-envelope"), nil
		}
		// must_have_real_validation_signal is satisfiable only from a real,
		// allowlisted, provenance-backed record (Task 139).
		if g.Config.Thresholds.MustHaveRealValidationSignal {
			ok, verr := g.realSignal().HasAllowlistedRealSignal(ctx, in.OpportunityID)
			if verr != nil {
				return RequireBuildVerdictOutput{}, fmt.Errorf("kernel: verify real signal: %w", verr)
			}
			if !ok {
				return refuse(state.ResultOpportunityVerdictMissing, "no-allowlisted-real-validation-signal"), nil
			}
		}
		out.Allowed = true
		out.Reason = "build-verdict-satisfied"
		out.WorkflowStatus = string(state.StatusRunning)
		return out, nil

	default:
		return refuse(state.ResultOpportunityVerdictUnreproducible, "unknown-verdict"), nil
	}
}
