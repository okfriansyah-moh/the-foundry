package kernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

const oppConfigPath = "../../config/opportunity-thresholds.yaml"

type fakeLoader struct {
	opp        opportunity.Opportunity
	rec        opportunity.VerdictRecord
	verdictErr error
	oppErr     error
}

func (f fakeLoader) LatestVerdict(context.Context, string) (opportunity.VerdictRecord, error) {
	if f.verdictErr != nil {
		return opportunity.VerdictRecord{}, f.verdictErr
	}
	return f.rec, nil
}

func (f fakeLoader) LoadOpportunity(context.Context, string) (opportunity.Opportunity, error) {
	if f.oppErr != nil {
		return opportunity.Opportunity{}, f.oppErr
	}
	return f.opp, nil
}

type fakeReserver struct{ amt float64 }

func (r *fakeReserver) Reserve(_ context.Context, amt float64, _ any) (string, error) {
	r.amt = amt
	return "resv-1", nil
}

type allowRealSignal struct{}

func (allowRealSignal) HasAllowlistedRealSignal(context.Context, string) (bool, error) {
	return true, nil
}

func loadOppConfig(t *testing.T) opportunity.Config {
	t.Helper()
	cfg, err := opportunity.LoadConfig(oppConfigPath)
	if err != nil {
		t.Fatalf("load opportunity config: %v", err)
	}
	return cfg
}

func observedClaim(kind opportunity.ClaimKind) opportunity.Claim {
	return opportunity.Claim{Kind: kind, Text: string(kind) + " evidence", Label: opportunity.LabelObserved, SourceRef: "src://" + string(kind) + "#h"}
}

func buildOpportunity() opportunity.Opportunity {
	return opportunity.Opportunity{
		Idea: opportunity.Idea{ID: "opp-1", Statement: "A tool solving a real recurring pain for a reachable segment."},
		ICP:  opportunity.ICP{Segment: "x", ReachableChannels: []opportunity.Channel{{Name: "c", Reachable: true}}},
		Claims: []opportunity.Claim{
			observedClaim(opportunity.KindProblem),
			observedClaim(opportunity.KindFrequency),
			observedClaim(opportunity.KindWTP),
			observedClaim(opportunity.KindDistribution),
			observedClaim(opportunity.KindMarket),
			observedClaim(opportunity.KindCompetitor),
			observedClaim(opportunity.KindAlternative),
		},
		EstimatedValidationCostUSD: 50,
		MVPBudgetUSD:               120,
		MaxActiveBuilds:            1,
		RealValidationSignal:       true,
	}
}

func inferredClaim(kind opportunity.ClaimKind) opportunity.Claim {
	return opportunity.Claim{Kind: kind, Text: string(kind), Label: opportunity.LabelInferred, SourceRef: "src://" + string(kind) + "#h"}
}

func validateMoreOpportunity() opportunity.Opportunity {
	return opportunity.Opportunity{
		Idea: opportunity.Idea{ID: "opp-2", Statement: "A modest helper for a niche."},
		ICP:  opportunity.ICP{Segment: "x", ReachableChannels: []opportunity.Channel{{Name: "c", Reachable: true}}},
		Claims: []opportunity.Claim{
			inferredClaim(opportunity.KindProblem),
			inferredClaim(opportunity.KindFrequency),
			inferredClaim(opportunity.KindWTP),
			inferredClaim(opportunity.KindDistribution),
			inferredClaim(opportunity.KindMarket),
		},
		EstimatedValidationCostUSD: 30,
		MVPBudgetUSD:               130,
		MaxActiveBuilds:            1,
		RealValidationSignal:       true,
	}
}

func rejectOpportunity() opportunity.Opportunity {
	return opportunity.Opportunity{
		Idea:                       opportunity.Idea{ID: "opp-3", Statement: "An app powered by AI that does everything."},
		ICP:                        opportunity.ICP{},
		Claims:                     []opportunity.Claim{{Kind: opportunity.KindMarket, Text: "big", Label: opportunity.LabelUnresolved}},
		EstimatedValidationCostUSD: 400,
		MVPBudgetUSD:               500,
	}
}

// honestVerdict computes the verdict record the way the store would, so
// re-derivation in the gate reproduces it exactly.
func honestVerdict(t *testing.T, cfg opportunity.Config, opp opportunity.Opportunity, at time.Time) opportunity.VerdictRecord {
	t.Helper()
	sc := opportunity.Score(opp, cfg)
	v, _ := opportunity.Decide(sc, cfg.Thresholds)
	digest, err := sc.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return opportunity.VerdictRecord{
		OpportunityID:   opp.Idea.ID,
		Verdict:         v,
		ScorecardDigest: digest,
		ConfigVersion:   cfg.Version,
		Scorecard:       sc,
		Thresholds:      cfg.Thresholds,
		CreatedAt:       at,
	}
}

func TestRequireBuildVerdict(t *testing.T) {
	cfg := loadOppConfig(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	buildOpp := buildOpportunity()
	buildRec := honestVerdict(t, cfg, buildOpp, now)
	if buildRec.Verdict != opportunity.VerdictBuild {
		t.Fatalf("fixture must be BUILD, got %q", buildRec.Verdict)
	}
	validateOpp := validateMoreOpportunity()
	validateRec := honestVerdict(t, cfg, validateOpp, now)
	if validateRec.Verdict != opportunity.VerdictValidateMore {
		t.Fatalf("fixture must be VALIDATE-MORE, got %q", validateRec.Verdict)
	}
	rejectOpp := rejectOpportunity()
	rejectRec := honestVerdict(t, cfg, rejectOpp, now)
	if rejectRec.Verdict != opportunity.VerdictReject {
		t.Fatalf("fixture must be REJECT, got %q", rejectRec.Verdict)
	}

	t.Run("missing verdict => MISSING/FAILED", func(t *testing.T) {
		g := &kernel.OpportunityGate{Loader: fakeLoader{verdictErr: opportunity.ErrNotFound}, Config: cfg, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictMissing), "verdict-missing", string(state.StatusFailed))
	})

	t.Run("expired verdict => MISSING/FAILED", func(t *testing.T) {
		old := honestVerdict(t, cfg, buildOpp, now.Add(-1000*time.Hour))
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: old}, Config: cfg, RealSignal: allowRealSignal{}, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictMissing), "verdict-expired", string(state.StatusFailed))
	})

	t.Run("digest mismatch => UNREPRODUCIBLE/FAILED", func(t *testing.T) {
		bad := buildRec
		bad.ScorecardDigest = "deadbeef"
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: bad}, Config: cfg, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictUnreproducible), "scorecard-digest-mismatch", string(state.StatusFailed))
	})

	t.Run("config drift => UNREPRODUCIBLE/FAILED", func(t *testing.T) {
		bad := buildRec
		bad.ConfigVersion = "stale-version"
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: bad}, Config: cfg, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictUnreproducible), "config-version-drift", string(state.StatusFailed))
	})

	t.Run("REJECT => OPPORTUNITY_REJECTED/SUCCEEDED, nothing reserved", func(t *testing.T) {
		res := &fakeReserver{}
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: rejectOpp, rec: rejectRec}, Config: cfg, Reserver: res, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-3"})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityRejected), "opportunity-rejected", string(state.StatusSucceeded))
		if out.Allowed {
			t.Fatal("REJECT must not allow a build")
		}
		if res.amt != 0 {
			t.Fatalf("REJECT must reserve nothing, reserved %.2f", res.amt)
		}
	})

	t.Run("VALIDATE-MORE => reserves capped envelope, SUCCEEDED", func(t *testing.T) {
		res := &fakeReserver{}
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: validateOpp, rec: validateRec}, Config: cfg, Reserver: res, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-2"})
		if err != nil {
			t.Fatal(err)
		}
		if out.ResultCode != string(state.ResultOpportunityValidationRequired) || out.WorkflowStatus != string(state.StatusSucceeded) {
			t.Fatalf("unexpected outcome: %+v", out)
		}
		if out.Allowed {
			t.Fatal("VALIDATE-MORE must not allow a build")
		}
		want := validateOpp.EstimatedValidationCostUSD
		if cap := cfg.Thresholds.ValidationCostCapUSD; cap > 0 && want > cap {
			want = cap
		}
		if res.amt != want || out.ReservedValidationUSD != want {
			t.Fatalf("reserved %.2f (out %.2f), want %.2f", res.amt, out.ReservedValidationUSD, want)
		}
		if out.ReservationID == "" {
			t.Fatal("expected a reservation id")
		}
	})

	t.Run("BUILD with real signal => allowed, RUNNING", func(t *testing.T) {
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: buildRec}, Config: cfg, RealSignal: allowRealSignal{}, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1", MissionEnvelopeUSD: 1000})
		if err != nil {
			t.Fatal(err)
		}
		if !out.Allowed || out.WorkflowStatus != string(state.StatusRunning) {
			t.Fatalf("BUILD with real signal must be allowed and RUNNING: %+v", out)
		}
	})

	t.Run("BUILD without real signal => refused (fail-closed default)", func(t *testing.T) {
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: buildRec}, Config: cfg, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1", MissionEnvelopeUSD: 1000})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictMissing), "no-allowlisted-real-validation-signal", string(state.StatusFailed))
		if out.Allowed {
			t.Fatal("BUILD without a real signal must not be allowed")
		}
	})

	t.Run("BUILD budget exceeds mission envelope => refused", func(t *testing.T) {
		g := &kernel.OpportunityGate{Loader: fakeLoader{opp: buildOpp, rec: buildRec}, Config: cfg, RealSignal: allowRealSignal{}, Now: nowFn}
		out, err := g.RequireBuildVerdict(context.Background(), kernel.RequireBuildVerdictInput{OpportunityID: "opp-1", MissionEnvelopeUSD: 10})
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, out, string(state.ResultOpportunityVerdictMissing), "mvp-budget-exceeds-mission-envelope", string(state.StatusFailed))
	})
}

func assertRefusal(t *testing.T, out kernel.RequireBuildVerdictOutput, code, reason, status string) {
	t.Helper()
	if out.ResultCode != code {
		t.Fatalf("result_code=%q, want %q (out=%+v)", out.ResultCode, code, out)
	}
	if out.Reason != reason {
		t.Fatalf("reason=%q, want %q", out.Reason, reason)
	}
	if out.WorkflowStatus != status {
		t.Fatalf("status=%q, want %q", out.WorkflowStatus, status)
	}
	// Every registered result code must map to the workflow status the gate
	// reports (registry consistency).
	if st, ok := state.KnownResultCode(state.ResultCode(code)); !ok || string(st) != status {
		t.Fatalf("result_code %q not registered on status %q", code, status)
	}
}
