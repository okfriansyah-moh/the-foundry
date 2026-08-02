package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/intake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity/signals"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// buildIntakeDeps chooses the offline cassette path (CLI fixture flags) or the
// production path (no fixture flags; Temporal + real-signal gate + signed
// approval) per docs/PLAN.md Task 144.
func buildIntakeDeps(store intake.Store, f *intakeFlags, db *sql.DB) (intake.Deps, error) {
	offline := f.dryRun || f.opportunityFixture != "" || f.specCassette != ""
	if offline {
		return buildOfflineIntakeDeps(store, f)
	}
	return buildProductionIntakeDeps(store, f, db)
}

func buildOfflineIntakeDeps(store intake.Store, f *intakeFlags) (intake.Deps, error) {
	if f.opportunityFixture == "" || f.specCassette == "" {
		return intake.Deps{}, errors.New("mission start --idea: offline path requires --opportunity-fixtures and --spec-cassette (omit both for production)")
	}
	cfg, err := opportunity.LoadConfig(f.opportunityConfig)
	if err != nil {
		return intake.Deps{}, fmt.Errorf("mission start --idea: load opportunity config: %w", err)
	}
	replay, err := spec.LoadReplaySource(f.specCassette)
	if err != nil {
		return intake.Deps{}, fmt.Errorf("mission start --idea: load spec cassette: %w", err)
	}

	starter := intake.FuncStarter(func(_ context.Context, in intake.StartMissionInput) (intake.StartMissionOutput, error) {
		if f.dryRun || f.temporalHostPort == "" {
			return intake.StartMissionOutput{MissionID: "draft-" + in.RunID}, nil
		}
		return startMissionViaTemporal(f, in)
	})

	approver := intake.FuncApprover(func(_ context.Context, in intake.ApproveInput) (intake.ApproveOutput, error) {
		return intake.ApproveOutput{ApprovalRef: "cli-approved:" + in.RunID}, nil
	})

	return intake.Deps{
		Store:     store,
		Validator: intake.OpportunityValidatorAdapter{Config: cfg, Resolver: intake.FileOpportunityResolver{Dir: f.opportunityFixture}},
		Synth:     intake.SpecSynthesizerAdapter{Synth: spec.Synthesizer{Source: replay}},
		PlanGen: intake.PlanGeneratorAdapter{Mission: spec.MissionContext{
			RepoAlias:       f.repoAlias,
			RepoURL:         f.repoURL,
			RepoBranch:      f.repoBranch,
			RepoWriteTarget: f.repoWriteTarget,
		}},
		Admitter:  intake.AdmitterAdapter{Policy: admission.NoopPolicyView{}},
		Approver:  approver,
		Readiness: intake.AlwaysReady,
		Starter:   starter,
	}, nil
}

func buildProductionIntakeDeps(store intake.Store, f *intakeFlags, db *sql.DB) (intake.Deps, error) {
	if db == nil {
		return intake.Deps{}, errors.New("mission start --idea: production path requires Postgres (--pg-dsn / PG_DSN)")
	}
	if f.temporalHostPort == "" {
		return intake.Deps{}, errors.New("mission start --idea: production path requires Temporal (--temporal-hostport / TEMPORAL_HOSTPORT)")
	}

	cfg, err := opportunity.LoadConfig(f.opportunityConfig)
	if err != nil {
		return intake.Deps{}, fmt.Errorf("mission start --idea: load opportunity config: %w", err)
	}
	signalAllowlist, err := signals.LoadAllowlist(envOr("FOUNDRY_VALIDATION_SIGNAL_ALLOWLIST", "config/validation-signal-allowlist.yaml"))
	if err != nil {
		return intake.Deps{}, fmt.Errorf("mission start --idea: load validation-signal allowlist: %w", err)
	}

	oppStore := opportunity.NewStore(db)
	baseValidator := intake.OpportunityValidatorAdapter{
		Config:   cfg,
		Resolver: statementOpportunityResolver{Store: oppStore},
	}
	validator := intake.SignalBackedValidator{
		Inner: baseValidator,
		RealSignal: kernel.StoreRealSignalVerifier{
			Store:     &signals.PGStore{DB: db},
			Allowlist: signalAllowlist,
		},
		OpportunityIDForIdea: func(idea string) string {
			id, err := oppStore.LookupIDByStatement(context.Background(), idea)
			if err != nil {
				return ""
			}
			return id
		},
	}

	synthSource, err := productionSpecSource()
	if err != nil {
		return intake.Deps{}, err
	}

	approver, err := productionApprover(db)
	if err != nil {
		return intake.Deps{}, err
	}

	starter := intake.ProductionStarter{
		StartFn: func(_ context.Context, in intake.StartMissionInput) (intake.StartMissionOutput, error) {
			return startMissionViaTemporal(f, in)
		},
	}

	return intake.Deps{
		Store:     store,
		Validator: validator,
		Synth:     intake.SpecSynthesizerAdapter{Synth: spec.Synthesizer{Source: synthSource}},
		PlanGen: intake.PlanGeneratorAdapter{Mission: spec.MissionContext{
			RepoAlias:       f.repoAlias,
			RepoURL:         f.repoURL,
			RepoBranch:      f.repoBranch,
			RepoWriteTarget: f.repoWriteTarget,
		}},
		Admitter:  intake.AdmitterAdapter{Policy: admission.NoopPolicyView{}},
		Approver:  approver,
		Readiness: intake.AlwaysReady,
		Starter:   starter,
	}, nil
}

// statementOpportunityResolver loads a persisted opportunity whose statement
// equals the idea text. Production research must have written the row; absence
// fails closed rather than inventing evidence.
type statementOpportunityResolver struct {
	Store *opportunity.Store
}

func (r statementOpportunityResolver) Resolve(ctx context.Context, idea string) (opportunity.Opportunity, error) {
	if r.Store == nil {
		return opportunity.Opportunity{}, fmt.Errorf("intake: production opportunity store not configured")
	}
	id, err := r.Store.LookupIDByStatement(ctx, idea)
	if err != nil {
		return opportunity.Opportunity{}, fmt.Errorf("intake: production opportunity resolve: %w", err)
	}
	return r.Store.LoadOpportunity(ctx, id)
}

func productionSpecSource() (spec.CandidateSource, error) {
	if cassette := strings.TrimSpace(os.Getenv("FOUNDRY_SPEC_CASSETTE")); cassette != "" {
		return spec.LoadReplaySource(cassette)
	}
	return nil, errors.New("mission start --idea: production path requires FOUNDRY_SPEC_CASSETTE until a live CandidateSource adapter is wired into the CLI (fail-closed; no invented requirements)")
}

func productionApprover(db *sql.DB) (intake.Approver, error) {
	dir, err := provenance.DefaultKeyDir()
	if err != nil {
		return nil, fmt.Errorf("mission start --idea: approver keys: %w", err)
	}
	kp, err := provenance.LoadKeyPair(dir)
	if err != nil {
		return nil, fmt.Errorf("mission start --idea: load approver keys (required on production path): %w", err)
	}
	allow, err := provenance.LoadAllowList(envOr("FOUNDRY_PERMISSIONS_ALLOWLIST", "config/permissions-allowlist.yaml"))
	if err != nil {
		return nil, fmt.Errorf("mission start --idea: load allowlist: %w", err)
	}
	raw := provenance.NewPGRawStore(db)
	pstore := provenance.NewStore(raw, kp.Public)
	principal := envOr("FOUNDRY_PRINCIPAL", "cli:operator")

	return intake.FuncApprover(func(ctx context.Context, in intake.ApproveInput) (intake.ApproveOutput, error) {
		doc, err := plan.ParseBytes(in.PlanBytes)
		if err != nil {
			return intake.ApproveOutput{}, fmt.Errorf("intake approve: parse plan: %w", err)
		}
		now := time.Now().UTC()
		var tier admission.Tier
		switch strings.ToUpper(strings.TrimSpace(in.Tier)) {
		case "A0":
			tier = admission.TierA0
		case "A1":
			tier = admission.TierA1
		case "A2":
			tier = admission.TierA2
		case "H":
			tier = admission.TierH
		default:
			tier = admission.TierA0
		}
		approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
			PlanID:              doc.ID,
			PlanDigest:          doc.DigestHex(),
			CreatorPrincipal:    principal,
			SubmittingPrincipal: principal,
			ClassifierVersion:   "intake-production",
			Requested:           doc.RequestedPermissions,
			RiskTier:            tier,
			BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: doc.BudgetUSD, WorkflowUSD: doc.BudgetUSD},
			DataClass:           "internal",
			Approvers:           []provenance.Approver{{Principal: principal, Method: provenance.AuthMethodEd25519Local, At: now}},
			ApprovedAt:          now,
			ExpiresAt:           now.Add(30 * 24 * time.Hour),
		}, allow)
		if err != nil {
			return intake.ApproveOutput{}, err
		}
		if err := provenance.Sign(kp.Private, approved); err != nil {
			return intake.ApproveOutput{}, err
		}
		if err := pstore.Insert(ctx, approved); err != nil {
			return intake.ApproveOutput{}, err
		}
		return intake.ApproveOutput{ApprovalRef: approved.PlanID()}, nil
	}), nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// ideaContentDigest is retained for diagnostics; opportunity binding uses
// exact statement match via LookupIDByStatement.
func ideaContentDigest(idea string) string {
	sum := sha256.Sum256([]byte(idea))
	return hex.EncodeToString(sum[:])
}
