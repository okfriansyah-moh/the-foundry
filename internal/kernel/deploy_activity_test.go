package kernel_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
)

// fakeExternalOps is an in-memory ExternalOpStore keyed by idempotency key, so
// the DeployProduct idempotency path (reserve → execute → reconcile-on-retry)
// is testable without Postgres.
type fakeExternalOps struct {
	mu  sync.Mutex
	ops map[string]*extops.Op
}

func newFakeExternalOps() *fakeExternalOps { return &fakeExternalOps{ops: map[string]*extops.Op{}} }

func (f *fakeExternalOps) Reserve(_ context.Context, workflowID, kind, target, key string, request any) (extops.Op, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if op, ok := f.ops[key]; ok {
		return *op, nil
	}
	raw, _ := json.Marshal(request)
	op := &extops.Op{ID: extops.OpID(key), WorkflowID: workflowID, Kind: kind, Target: target, IdempotencyKey: key, State: extops.StateReserved, Request: raw}
	f.ops[key] = op
	return *op, nil
}

func (f *fakeExternalOps) MarkExecuted(_ context.Context, id extops.OpID, receipt any) (extops.Op, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	op, ok := f.ops[string(id)]
	if !ok {
		return extops.Op{}, extops.ErrOpNotFound
	}
	raw, _ := json.Marshal(receipt)
	op.State = extops.StateExecuted
	op.Receipt = raw
	return *op, nil
}

// fakeAdapter is a deploy.Adapter that records calls and can be made unhealthy.
type fakeAdapter struct {
	mu        sync.Mutex
	deploys   int
	rollbacks int
	unhealthy bool
	healthURL string
}

func (a *fakeAdapter) DeployPreview(_ context.Context, product, artifact string) (deploy.Record, error) {
	return a.record(product, artifact, "preview"), nil
}

func (a *fakeAdapter) DeployProduction(_ context.Context, product, artifact string) (deploy.Record, error) {
	a.mu.Lock()
	a.deploys++
	a.mu.Unlock()
	return a.record(product, artifact, "production"), nil
}

func (a *fakeAdapter) record(product, artifact, env string) deploy.Record {
	url := a.healthURL
	if url == "" {
		url = "https://" + product + ".example"
	}
	return deploy.Record{Product: product, Environment: env, Ref: artifact, URL: url}
}

func (a *fakeAdapter) Rollback(_ context.Context, product, ref string) (deploy.Record, error) {
	a.mu.Lock()
	a.rollbacks++
	a.mu.Unlock()
	return deploy.Record{Product: product, Environment: "production", Ref: ref}, nil
}

func (a *fakeAdapter) Health(_ context.Context, _ string) error {
	if a.unhealthy {
		return errors.New("unhealthy")
	}
	return nil
}

func passingGate() deploy.GateInput {
	return deploy.GateInput{
		PersonalProfile: true, DeploymentTargetAllowlisted: true, MissionReadinessComplete: true,
		SpendWithinEnvelope: true, DeterministicVerificationPassed: true, SyntheticOrRealCanaryPassed: true,
		RollbackRehearsed: true, DatabaseChangesReversibleOrBackwardCompatible: true, NoRegulatedData: true,
		NoNewSecretScope: true, NoAuthorityExpansion: true, HealthChecksDefined: true, OperationReconciliationEnabled: true,
	}
}

func deployInput(key string, gate deploy.GateInput) kernel.DeployProductInput {
	return kernel.DeployProductInput{
		WorkflowID: "wf1", IdempotencyKey: key, Product: "demo", Environment: "production",
		Artifact: "img-1", PreviousRef: "img-0", HealthURL: "https://demo.example",
		Profile: "personal", GrantedEnvironments: []string{"production"}, Gate: gate,
	}
}

func TestDeployProduct_HealthyDeploy(t *testing.T) {
	adapter := &fakeAdapter{}
	acts := &kernel.Activities{DeployAdapter: adapter, ExternalOps: newFakeExternalOps()}
	out, err := acts.DeployProduct(context.Background(), deployInput("k1", passingGate()))
	if err != nil {
		t.Fatalf("DeployProduct: %v", err)
	}
	if !out.Deployed || out.ResultCode != deploy.ResultDeployed {
		t.Fatalf("out = %+v, want Deployed/%s", out, deploy.ResultDeployed)
	}
	if adapter.rollbacks != 0 {
		t.Fatalf("healthy deploy must not roll back, rollbacks=%d", adapter.rollbacks)
	}
}

func TestDeployProduct_UnhealthyRollsBack(t *testing.T) {
	adapter := &fakeAdapter{unhealthy: true}
	acts := &kernel.Activities{DeployAdapter: adapter, ExternalOps: newFakeExternalOps()}
	out, err := acts.DeployProduct(context.Background(), deployInput("k1", passingGate()))
	if err != nil {
		t.Fatalf("DeployProduct: %v", err)
	}
	if out.ResultCode != deploy.ResultUnhealthyRolledBack || !out.Receipt.RolledBack {
		t.Fatalf("out = %+v, want unhealthy-rolled-back", out)
	}
	if adapter.rollbacks != 1 {
		t.Fatalf("unhealthy deploy must roll back exactly once, rollbacks=%d", adapter.rollbacks)
	}
}

func TestDeployProduct_Idempotent(t *testing.T) {
	adapter := &fakeAdapter{}
	acts := &kernel.Activities{DeployAdapter: adapter, ExternalOps: newFakeExternalOps()}
	if _, err := acts.DeployProduct(context.Background(), deployInput("k1", passingGate())); err != nil {
		t.Fatalf("deploy 1: %v", err)
	}
	if _, err := acts.DeployProduct(context.Background(), deployInput("k1", passingGate())); err != nil {
		t.Fatalf("deploy 2 (retry): %v", err)
	}
	if adapter.deploys != 1 {
		t.Fatalf("retried deploy must reconcile against the receipt, deploys=%d want 1", adapter.deploys)
	}
}

func TestDeployProduct_GateCommandModeBlocks(t *testing.T) {
	adapter := &fakeAdapter{}
	acts := &kernel.Activities{DeployAdapter: adapter, ExternalOps: newFakeExternalOps()}
	gate := passingGate()
	gate.RollbackRehearsed = false // one unmet requirement → command mode
	out, err := acts.DeployProduct(context.Background(), deployInput("k1", gate))
	if err != nil {
		t.Fatalf("DeployProduct: %v", err)
	}
	if out.Deployed || out.ResultCode != deploy.ResultGateCommandPending {
		t.Fatalf("out = %+v, want gate-command-pending, no deploy", out)
	}
	if adapter.deploys != 0 {
		t.Fatalf("a command-mode gate must not deploy, deploys=%d", adapter.deploys)
	}
}

func TestDeployProduct_EnvironmentNotGranted(t *testing.T) {
	adapter := &fakeAdapter{}
	acts := &kernel.Activities{DeployAdapter: adapter, ExternalOps: newFakeExternalOps()}
	in := deployInput("k1", passingGate())
	in.GrantedEnvironments = []string{"preview"} // production not granted
	out, err := acts.DeployProduct(context.Background(), in)
	if err != nil {
		t.Fatalf("DeployProduct: %v", err)
	}
	if out.Deployed || out.ResultCode != deploy.ResultOutsideGrantedEnvs {
		t.Fatalf("out = %+v, want environment-not-granted, no deploy", out)
	}
	if adapter.deploys != 0 {
		t.Fatalf("a deploy outside granted environments must not run, deploys=%d", adapter.deploys)
	}
}
