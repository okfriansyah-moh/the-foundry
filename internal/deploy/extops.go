package deploy

import (
	"context"
	"fmt"
)

// Result codes a deploy terminates with, so a caller branches on the outcome
// without string-matching (docs/PLAN.md Task 125).
const (
	ResultDeployed            = "DEPLOYED"
	ResultUnhealthyRolledBack = "DEPLOY_UNHEALTHY_ROLLED_BACK"
	ResultRollbackFailed      = "DEPLOY_ROLLBACK_FAILED"
	ResultGateBlocked         = "DEPLOY_GATE_BLOCKED"
	ResultGateCommandPending  = "DEPLOY_GATE_COMMAND_PENDING"
	ResultOutsideGrantedEnvs  = "DEPLOY_ENVIRONMENT_NOT_GRANTED"
	ResultQuotaExhausted      = "DEPLOY_QUOTA_EXHAUSTED"
)

// DeployRequest is the input to a single deploy operation.
type DeployRequest struct {
	Product     string
	Environment string // "production" | "preview"
	Artifact    string
	// PreviousRef is re-deployed if a production deploy's health check fails.
	PreviousRef string
	// HealthURL overrides the URL health-checked; empty uses the deploy
	// record's own returned URL.
	HealthURL string
}

// DeployReceipt is the durable outcome of one deploy — the shape recorded on
// the external-operation ledger, so a retried deploy reconciles against it
// instead of deploying twice (Constitution C9).
type DeployReceipt struct {
	Record     Record `json:"record"`
	Healthy    bool   `json:"healthy"`
	RolledBack bool   `json:"rolled_back"`
	ResultCode string `json:"result_code"`
}

// Execute performs one deploy through adapter and applies the health/rollback
// policy: deploy, health-check the real returned URL, and on an unhealthy
// production deploy trigger the recorded rollback, terminating with a named
// result code. It never leaves an unhealthy production deploy in place. This is
// the side-effecting function the kernel's DeployProduct activity wraps in
// WithExternalOp; it must run kernel-side, never in the executor sandbox.
func Execute(ctx context.Context, adapter Adapter, req DeployRequest) (DeployReceipt, error) {
	var (
		rec Record
		err error
	)
	switch req.Environment {
	case "preview":
		rec, err = adapter.DeployPreview(ctx, req.Product, req.Artifact)
	default:
		rec, err = adapter.DeployProduction(ctx, req.Product, req.Artifact)
	}
	if err != nil {
		return DeployReceipt{}, fmt.Errorf("deploy: %s %q: %w", req.Environment, req.Product, err)
	}

	healthURL := req.HealthURL
	if healthURL == "" {
		healthURL = rec.URL
	}
	if healthURL == "" {
		// No URL to health-check: cannot vouch for the deploy — treat as
		// unhealthy and roll back rather than assume success.
		return rollback(ctx, adapter, req, rec, fmt.Errorf("deploy: no health URL available"))
	}
	if herr := adapter.Health(ctx, healthURL); herr != nil {
		return rollback(ctx, adapter, req, rec, herr)
	}
	return DeployReceipt{Record: rec, Healthy: true, ResultCode: ResultDeployed}, nil
}

func rollback(ctx context.Context, adapter Adapter, req DeployRequest, rec Record, cause error) (DeployReceipt, error) {
	if _, rbErr := adapter.Rollback(ctx, req.Product, req.PreviousRef); rbErr != nil {
		// The deploy is unhealthy AND rollback failed — the worst case must be
		// surfaced honestly with its own code, not hidden.
		return DeployReceipt{Record: rec, Healthy: false, RolledBack: false, ResultCode: ResultRollbackFailed},
			fmt.Errorf("deploy: unhealthy (%v) and rollback failed: %w", cause, rbErr)
	}
	return DeployReceipt{Record: rec, Healthy: false, RolledBack: true, ResultCode: ResultUnhealthyRolledBack}, nil
}
