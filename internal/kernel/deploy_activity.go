package kernel

import (
	"context"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
)

// ActivityDeployProduct is the kernel-owned production deploy activity
// (docs/PLAN.md Task 125 / VEN-15).
const ActivityDeployProduct = "DeployProduct"

// DeployProductInput drives one production deploy. The gate inputs and the
// profile's granted environments are supplied by the caller; the kernel decides
// whether to deploy (Constitution C4/C13).
type DeployProductInput struct {
	WorkflowID     string
	IdempotencyKey string
	Product        string
	Environment    string // "production" | "preview"
	Artifact       string
	PreviousRef    string
	HealthURL      string
	Profile        string
	// GrantedEnvironments is the personal profile's bounded production-auto
	// grant (C13). A deploy to an environment not in this set is refused.
	GrantedEnvironments []string
	// Gate is the 13-field commercial-readiness gate input (Task 47's
	// EvaluateGate, unchanged). A gate in command mode blocks the deploy
	// pending human approval rather than deploying.
	Gate deploy.GateInput
}

// DeployProductOutput reports the deploy outcome. A non-deployed outcome
// (gate-blocked, command-pending, quota-exhausted, environment-not-granted)
// carries a named ResultCode and Deployed=false; the workflow acts on it
// without treating it as an infra error.
type DeployProductOutput struct {
	Deployed   bool
	Receipt    deploy.DeployReceipt
	ResultCode string
	GateMode   string
}

// DeployProduct performs a policy-gated, quota-bounded, idempotency-keyed,
// receipt-backed production deploy. It is the ONLY path a product deploy takes,
// and it runs kernel-side with a scoped credential — neither the Fly API nor
// flyctl ever runs in the executor sandbox (C4/C9/C13). A retried deploy
// reconciles against the extops receipt instead of deploying twice.
func (a *Activities) DeployProduct(ctx context.Context, in DeployProductInput) (DeployProductOutput, error) {
	if a.DeployAdapter == nil || a.ExternalOps == nil {
		return DeployProductOutput{}, fmt.Errorf("kernel: DeployProduct not wired (adapter/extops missing)")
	}

	// (1) The environment must be within the profile's bounded grant (C13).
	if !environmentGranted(in.Environment, in.GrantedEnvironments) {
		return DeployProductOutput{ResultCode: deploy.ResultOutsideGrantedEnvs}, nil
	}

	// (2) The 13-field readiness gate must pass. A command-mode result waits
	// for human approval — it does NOT deploy.
	gate := deploy.EvaluateGate(in.Gate)
	if !gate.Passed {
		code := deploy.ResultGateBlocked
		if gate.Mode == "command" {
			code = deploy.ResultGateCommandPending
		}
		return DeployProductOutput{ResultCode: code, GateMode: gate.Mode}, nil
	}

	// (3) Quota: a deploy consumes the profile's admission budget so a mission
	// cannot deploy in a loop (Task 125). CanAcquire is a pure check; the
	// deploy itself is the bounded action.
	if a.DeployQuota != nil && in.Profile != "" {
		if err := a.DeployQuota.CanAcquire(in.Profile, deploy.Usage{Admissions: 1}); err != nil {
			return DeployProductOutput{ResultCode: deploy.ResultQuotaExhausted}, nil
		}
	}

	// (4) The deploy runs as an external operation: idempotency key + receipt,
	// so a retry reconciles instead of re-deploying (C9). Execute applies the
	// health-check-and-rollback policy inside the side-effect fn.
	req := deploy.DeployRequest{
		Product:     in.Product,
		Environment: in.Environment,
		Artifact:    in.Artifact,
		PreviousRef: in.PreviousRef,
		HealthURL:   in.HealthURL,
	}
	receipt, err := WithExternalOp(ctx, a.ExternalOps, in.WorkflowID, "deploy."+in.Environment, in.Product, in.IdempotencyKey, req,
		func(opCtx context.Context) (deploy.DeployReceipt, error) {
			if a.DeployQuota != nil && in.Profile != "" {
				if aerr := a.DeployQuota.Acquire(in.Profile, deploy.Usage{Admissions: 1}); aerr != nil {
					return deploy.DeployReceipt{}, aerr
				}
			}
			return deploy.Execute(opCtx, a.DeployAdapter, req)
		})
	if err != nil {
		return DeployProductOutput{}, err
	}
	return DeployProductOutput{Deployed: true, Receipt: receipt, ResultCode: receipt.ResultCode, GateMode: gate.Mode}, nil
}

func environmentGranted(env string, granted []string) bool {
	for _, g := range granted {
		if g == env {
			return true
		}
	}
	return false
}
