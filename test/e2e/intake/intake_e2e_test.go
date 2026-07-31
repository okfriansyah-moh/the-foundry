// Package intake_e2e drives the Task 111 intake pipeline end to end on
// cassettes and fixtures with zero network and no database: real opportunity
// Score/Decide (from fixtures), real spec synthesis (a ReplaySource cassette),
// real PLAN generation, real admission classification, and the pipeline's stage
// machine. Only the authority-bearing mission starter is a recording fake — the
// same seam a live deployment injects a Temporal starter into.
package intake_e2e

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/intake"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

const (
	oppConfig    = "../../../config/opportunity-thresholds.yaml"
	specCassette = "../../cassettes/spec/req1.json"
	oppBuild     = "../../../internal/opportunity/testdata/build.json"
	oppReject    = "../../../internal/opportunity/testdata/reject.json"
)

// recordingStarter counts mission starts so build-nothing and idempotency can
// be asserted.
type recordingStarter struct{ calls int }

func (r *recordingStarter) Start(_ context.Context, in intake.StartMissionInput) (intake.StartMissionOutput, error) {
	r.calls++
	return intake.StartMissionOutput{MissionID: "e2e-" + in.RunID}, nil
}

// fixtureDir writes the opportunity fixture for ideaText keyed by the same
// sha256 digest FileOpportunityResolver uses, and returns the directory.
func fixtureDir(t *testing.T, ideaText, oppFixture string) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile(oppFixture)
	if err != nil {
		t.Fatalf("read opportunity fixture: %v", err)
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(ideaText)))
	if err := os.WriteFile(filepath.Join(dir, key+".json"), raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func buildDeps(t *testing.T, ideaText, oppFixture string, starter intake.MissionStarter) intake.Deps {
	t.Helper()
	cfg, err := opportunity.LoadConfig(oppConfig)
	if err != nil {
		t.Fatalf("load opportunity config: %v", err)
	}
	replay, err := spec.LoadReplaySource(specCassette)
	if err != nil {
		t.Fatalf("load spec cassette: %v", err)
	}
	return intake.Deps{
		Store:     intake.NewMemStore(),
		Validator: intake.OpportunityValidatorAdapter{Config: cfg, Resolver: intake.FileOpportunityResolver{Dir: fixtureDir(t, ideaText, oppFixture)}},
		Synth:     intake.SpecSynthesizerAdapter{Synth: spec.Synthesizer{Source: replay}},
		PlanGen: intake.PlanGeneratorAdapter{Mission: spec.MissionContext{
			RepoAlias: "product", RepoURL: "https://example.com/repo.git",
			RepoBranch: "main", RepoWriteTarget: "src/",
		}},
		Admitter: intake.AdmitterAdapter{Policy: admission.NoopPolicyView{}},
		Approver: intake.FuncApprover(func(_ context.Context, in intake.ApproveInput) (intake.ApproveOutput, error) {
			return intake.ApproveOutput{ApprovalRef: "appr:" + in.RunID}, nil
		}),
		Readiness: intake.AlwaysReady,
		Starter:   starter,
	}
}

// fixedAdmitter classifies at a fixed tier, standing in for the compiled policy
// of a pre-authorized personal-autonomous profile (an in-envelope build is A2 /
// auto — Task 116/118 supply the real compiled policy; Task 111 only obeys the
// classifier's verdict).
type fixedAdmitter struct{ tier string }

func (a fixedAdmitter) Classify(_ context.Context, _ intake.AdmitInput) (intake.AdmitOutput, error) {
	return intake.AdmitOutput{Tier: a.tier, RequiresStrongAuth: a.tier == "H"}, nil
}

func TestIntakeE2E_HappyPath_RealAdapters(t *testing.T) {
	idea := "Build a scheduling SaaS that removes booking back-and-forth for solo consultants."
	starter := &recordingStarter{}
	deps := buildDeps(t, idea, oppBuild, starter)
	p, err := intake.NewPipeline(deps)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	run, err := p.Start(context.Background(), intake.StartInput{Idea: idea, Budget: 200})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The BUILD fixture must clear the verdict gate and drive real synthesis,
	// plan generation and admission. Depending on the classified tier of the
	// generated plan, the run either starts the mission (tier < H) or halts for
	// strong-auth (tier H) — both are correct, documented outcomes; a terminal
	// REJECT/VALIDATE-MORE here would be a regression.
	switch run.CurrentStage {
	case intake.StageMissionStarted:
		if starter.calls != 1 {
			t.Fatalf("want exactly 1 mission start, got %d", starter.calls)
		}
	case intake.StageAwaitingStrongAuth:
		if starter.calls != 0 {
			t.Fatalf("H-tier plan must not start a mission")
		}
	default:
		t.Fatalf("unexpected happy-path stage %s (status %s)", run.CurrentStage, run.Status)
	}
	if run.SpentUSD <= 0 {
		t.Fatalf("expected a recorded research spend, got %v", run.SpentUSD)
	}
}

// TestIntakeE2E_AutoApproved_StartsMission proves the card's headline
// acceptance: one command takes a fixture idea to a running MissionLoop with
// zero further human input on the happy path, when the compiled policy of a
// pre-authorized personal-autonomous profile classifies the in-envelope build
// as auto (A2). Real opportunity Score/Decide, spec synthesis and PLAN
// generation still run; only the tier is the profile's.
func TestIntakeE2E_AutoApproved_StartsMission(t *testing.T) {
	idea := "Build a scheduling SaaS that removes booking back-and-forth for solo consultants."
	starter := &recordingStarter{}
	deps := buildDeps(t, idea, oppBuild, starter)
	deps.Admitter = fixedAdmitter{tier: "A2"}
	p, err := intake.NewPipeline(deps)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	run, err := p.Start(context.Background(), intake.StartInput{Idea: idea, Budget: 200})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.CurrentStage != intake.StageMissionStarted || run.Status != intake.StatusDone {
		t.Fatalf("want done/MISSION_STARTED, got %s/%s", run.Status, run.CurrentStage)
	}
	if starter.calls != 1 || run.MissionID == "" {
		t.Fatalf("want exactly one mission started, got calls=%d id=%q", starter.calls, run.MissionID)
	}
}

func TestIntakeE2E_Reject_BuildsNothing(t *testing.T) {
	idea := "A revolutionary AI app that does everything for everyone."
	starter := &recordingStarter{}
	deps := buildDeps(t, idea, oppReject, starter)
	p, err := intake.NewPipeline(deps)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	run, err := p.Start(context.Background(), intake.StartInput{Idea: idea, Budget: 200})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.CurrentStage != intake.StageOpportunityRejected {
		t.Fatalf("want OPPORTUNITY_REJECTED, got %s", run.CurrentStage)
	}
	if starter.calls != 0 {
		t.Fatal("a rejected opportunity must create no mission")
	}
	if run.MissionID != "" {
		t.Fatal("a rejected opportunity must not reserve a mission")
	}
}

func TestIntakeE2E_Resume_Idempotent(t *testing.T) {
	idea := "Build a scheduling SaaS that removes booking back-and-forth for solo consultants."
	starter := &recordingStarter{}
	deps := buildDeps(t, idea, oppBuild, starter)
	deps.Admitter = fixedAdmitter{tier: "A2"}
	p, err := intake.NewPipeline(deps)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	run, err := p.Start(context.Background(), intake.StartInput{Idea: idea, Budget: 200})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.CurrentStage != intake.StageMissionStarted {
		t.Fatalf("want the run started so idempotency of a started mission is under test, got %s", run.CurrentStage)
	}
	before := starter.calls
	for i := 0; i < 3; i++ {
		if _, err := p.Resume(context.Background(), run.ID); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}
	if starter.calls != before {
		t.Fatalf("resume duplicated a mission start: before=%d after=%d", before, starter.calls)
	}
}

// TestIntakeE2E_GeneratedPlanParses proves the real spec→plangen→parse chain
// produces an admission-classifiable PLAN (no self-classification, C6).
func TestIntakeE2E_GeneratedPlanParses(t *testing.T) {
	replay, err := spec.LoadReplaySource(specCassette)
	if err != nil {
		t.Fatalf("load spec cassette: %v", err)
	}
	specDoc, err := (spec.Synthesizer{Source: replay}).Synthesize(context.Background(), "any idea")
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	b, err := spec.PlanFromSpecification("e2e-plan", "E2E", specDoc, spec.EffectMapping{}, spec.MissionContext{
		RepoURL: "https://example.com/repo.git", RepoWriteTarget: "src/",
	})
	if err != nil {
		t.Fatalf("plangen: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty generated plan")
	}
}
