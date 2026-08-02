package envelope_test

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

type e2eRun struct{ id, runID string }

func (r e2eRun) GetID() string                                             { return r.id }
func (r e2eRun) GetRunID() string                                          { return r.runID }
func (r e2eRun) Get(context.Context, any) error                            { return nil }
func (r e2eRun) GetWithOptions(context.Context, any, client.WorkflowRunGetOptions) error {
	return nil
}

type e2eStarter struct{}

func (e2eStarter) ExecuteWorkflow(_ context.Context, opts client.StartWorkflowOptions, _ any, _ ...any) (client.WorkflowRun, error) {
	return e2eRun{id: opts.ID, runID: "run-e2e"}, nil
}

// TestEnvelopeLivePath_StartBindsDigestIntoTransition proves the production
// StartDelivery path persists an envelope and stamps its digest on the
// initial transition (docs/PLAN.md Task 141 Acceptance 21).
func TestEnvelopeLivePath_StartBindsDigestIntoTransition(t *testing.T) {
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID: "plan-e2e-env", PlanDigest: "digest-e2e-env",
		CreatorPrincipal: "alice", SubmittingPrincipal: "alice",
		ClassifierVersion: "v1",
		Scope:             provenance.Scope{Repositories: []string{"https://github.com/example/x"}},
		RiskTier:          admission.TierA0, ProfileKind: "personal",
		BudgetEnvelope: provenance.BudgetEnvelope{MonthlyUSD: 10, WorkflowUSD: 5},
		DataClass:      "internal",
		Approvers:      []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:     now, ExpiresAt: now.Add(time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatal(err)
	}
	if err := provenance.Sign(kp.Private, approved); err != nil {
		t.Fatal(err)
	}
	prov := provenance.NewStore(provenance.NewMemRawStore(), kp.Public)
	if err := prov.Insert(context.Background(), approved); err != nil {
		t.Fatal(err)
	}

	platform, err := compiler.PlatformDefaults()
	if err != nil {
		t.Fatal(err)
	}
	requireSandbox := true
	resolved, err := compiler.Compile(platform, compiler.LayerPolicy{}, compiler.LayerPolicy{RequireSandbox: &requireSandbox}, compiler.LayerPolicy{})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := observe.LoadQueueConfig("../../../config/queue-priority.yaml")
	if err != nil {
		t.Fatalf("queue config: %v", err)
	}

	transitions := kernel.NewMemTransitionStore()
	deps := kernel.StartDeps{
		Starter:           e2eStarter{},
		Provenance:        prov,
		LaneSelector:      kernel.LaneSelector{},
		QueueConfig:       cfg,
		ExecutorAllowlist: []string{"fake"},
		Transitions:       transitions,
		EnvelopeStore:     kernel.NewMemEnvelopeStore(),
		Policy:            resolved,
		Now:               func() time.Time { return now },
	}

	out, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{
		PlanID: "plan-e2e-env", MissionID: "mission-e2e", ProfileID: "personal-autonomous-venture",
		Unattended: true, RepositoryID: "repo", Provider: kernel.ProviderLocal,
		CanonicalURL: "file:///tmp/x", PinnedBaseRevision: "rev",
	})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	rows := transitions.All(out.WorkflowID)
	if len(rows) == 0 {
		t.Fatal("expected initial transition")
	}
	if rows[0].EnvelopeDigest != out.EnvelopeDigest || rows[0].EnvelopeDigest == "" {
		t.Fatalf("transition missing envelope digest: %+v", rows[0])
	}
	if rows[0].Status != state.StatusRunning {
		t.Fatalf("status = %s", rows[0].Status)
	}
}
