package kernel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

const queueConfigPath = "../../config/queue-priority.yaml"

// fakeRun is a minimal client.WorkflowRun.
type fakeRun struct{ id, runID string }

func (r fakeRun) GetID() string    { return r.id }
func (r fakeRun) GetRunID() string { return r.runID }
func (r fakeRun) Get(context.Context, any) error {
	return nil
}

func (r fakeRun) GetWithOptions(context.Context, any, client.WorkflowRunGetOptions) error {
	return nil
}

// fakeStarter records the options it was called with and can simulate a
// duplicate.
type fakeStarter struct {
	calls    int
	lastOpts client.StartWorkflowOptions
	lastArgs []any
	dupOnce  bool
}

func (f *fakeStarter) ExecuteWorkflow(_ context.Context, opts client.StartWorkflowOptions, _ any, args ...any) (client.WorkflowRun, error) {
	f.calls++
	f.lastOpts = opts
	f.lastArgs = args
	if f.dupOnce {
		return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
	}
	return fakeRun{id: opts.ID, runID: "run-1"}, nil
}

func newApprovedPlanStore(t *testing.T) (*provenance.Store, *provenance.KeyPair, string) {
	t.Helper()
	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}
	now := time.Now().UTC()
	approved, err := provenance.NewApprovedPlan(provenance.ApprovedPlanInput{
		PlanID:              "plan-start-1",
		PlanDigest:          "digest-abc-123",
		CreatorPrincipal:    "alice",
		SubmittingPrincipal: "alice",
		ClassifierVersion:   "v1",
		Scope:               provenance.Scope{Repositories: []string{"https://github.com/example/x"}},
		RiskTier:            admission.TierA0,
		BudgetEnvelope:      provenance.BudgetEnvelope{MonthlyUSD: 1, WorkflowUSD: 1},
		DataClass:           "internal",
		Approvers:           []provenance.Approver{{Principal: "alice", Method: provenance.AuthMethodEd25519Local, At: now}},
		ApprovedAt:          now,
		ExpiresAt:           now.Add(24 * time.Hour),
	}, provenance.AllowList{})
	if err != nil {
		t.Fatalf("new approved plan: %v", err)
	}
	if err := provenance.Sign(kp.Private, approved); err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw := provenance.NewMemRawStore()
	store := provenance.NewStore(raw, kp.Public)
	if err := store.Insert(context.Background(), approved); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return store, kp, approved.PlanDigest()
}

func loadQueueCfg(t *testing.T) observe.QueueConfig {
	t.Helper()
	cfg, err := observe.LoadQueueConfig(queueConfigPath)
	if err != nil {
		t.Fatalf("load queue config: %v", err)
	}
	return cfg
}

func baseDeps(t *testing.T, starter kernel.WorkflowStarter, store *provenance.Store) kernel.StartDeps {
	return kernel.StartDeps{
		Starter:           starter,
		Provenance:        store,
		QueueConfig:       loadQueueCfg(t),
		LaneSelector:      kernel.LaneSelector{},
		ExecutorAllowlist: []string{"fake"},
	}
}

func TestStartDeliverySuccess(t *testing.T) {
	store, _, digest := newApprovedPlanStore(t)
	starter := &fakeStarter{}
	out, err := kernel.StartDelivery(context.Background(), baseDeps(t, starter, store), kernel.StartDeliveryInput{PlanID: "plan-start-1"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if out.WorkflowID != kernel.DeliveryWorkflowID(digest, 0) {
		t.Fatalf("workflow id not deterministic: %s", out.WorkflowID)
	}
	if out.TaskQueue == "" || out.TaskQueue == "foundry-core" {
		t.Fatalf("task queue not lane-resolved: %q", out.TaskQueue)
	}
	if starter.lastOpts.ID != out.WorkflowID {
		t.Fatalf("started with wrong id: %s", starter.lastOpts.ID)
	}
	// The kernel resolved the allowlist into DeliverPlanInput.
	in, ok := starter.lastArgs[0].(kernel.DeliverPlanInput)
	if !ok || len(in.ExecutorAllowlist) == 0 {
		t.Fatalf("kernel must pass a non-nil executor allowlist: %+v", starter.lastArgs)
	}
}

func TestStartDeliveryDuplicateRejected(t *testing.T) {
	store, _, _ := newApprovedPlanStore(t)
	starter := &fakeStarter{dupOnce: true}
	_, err := kernel.StartDelivery(context.Background(), baseDeps(t, starter, store), kernel.StartDeliveryInput{PlanID: "plan-start-1"})
	if !errors.Is(err, kernel.ErrStartDuplicate) {
		t.Fatalf("expected ErrStartDuplicate, got %v", err)
	}
}

func TestStartDeliveryUnknownLaneRefused(t *testing.T) {
	store, _, _ := newApprovedPlanStore(t)
	deps := baseDeps(t, &fakeStarter{}, store)
	_, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{PlanID: "plan-start-1", Lane: "no-such-lane"})
	if !errors.Is(err, kernel.ErrStartRefused) {
		t.Fatalf("expected ErrStartRefused for unknown lane, got %v", err)
	}
}

func TestStartDeliveryEmptyAllowlistRefused(t *testing.T) {
	store, _, _ := newApprovedPlanStore(t)
	deps := baseDeps(t, &fakeStarter{}, store)
	deps.ExecutorAllowlist = nil
	_, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{PlanID: "plan-start-1"})
	if !errors.Is(err, kernel.ErrStartRefused) {
		t.Fatalf("expected ErrStartRefused for empty allowlist, got %v", err)
	}
}

func TestStartDeliveryRevokedPlanRefused(t *testing.T) {
	store, kp, _ := newApprovedPlanStore(t)
	if _, err := store.Revoke(context.Background(), "plan-start-1", kp.Private, "admin", "test"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	deps := baseDeps(t, &fakeStarter{}, store)
	_, err := kernel.StartDelivery(context.Background(), deps, kernel.StartDeliveryInput{PlanID: "plan-start-1"})
	if !errors.Is(err, kernel.ErrStartRefused) {
		t.Fatalf("expected ErrStartRefused for revoked plan, got %v", err)
	}
}

func TestDeliveryWorkflowIDStableAndAttemptSensitive(t *testing.T) {
	a := kernel.DeliveryWorkflowID("d1", 0)
	if a != kernel.DeliveryWorkflowID("d1", 0) {
		t.Fatal("workflow id not deterministic")
	}
	if a == kernel.DeliveryWorkflowID("d1", 1) {
		t.Fatal("attempt ordinal must change the id")
	}
	if a == kernel.DeliveryWorkflowID("d2", 0) {
		t.Fatal("plan digest must change the id")
	}
}
