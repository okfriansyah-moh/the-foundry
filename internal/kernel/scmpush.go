package kernel

import (
	"context"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// scmpush.go is the one file in this repository permitted to import
// internal/scm/write (Constitution C4; docs/PLAN.md Task 27/FND-08). Task
// 28's fitlint rule will make this compile-time-enforced; until then this
// package is simply, in fact, the only importer (bash scripts/
// check_scm_boundary.sh's current text-match check already covers the
// pre-split internal/scm import path — see internal/scm/write/doc.go).
//
// PushBranch below is exposed as a standalone function in this package —
// the same shape as WithExternalOp in externalop.go — rather than as a
// method wired into Activities/DeliverPlan's per-task loop. Task 27's
// Steps require the push protocol itself plus a demonstrable local-fixture
// e2e proof (`make e2e-github`), not full workflow-loop integration:
// branch delivery policy selection (docs/foundry/docs/workflows/
// multi-repository.md N10.2 — pull-request / direct-shared-branch /
// no-remote-write) is a distinct, not-yet-built concern. A future task can
// wire an Activities.PushBranch method around this function once that
// policy selection exists, without changing anything here.

// leaseAdapter adapts this package's LeaseStore (whose Acquire returns a
// Lease struct) to internal/scm/write.LeaseAcquirer's (token string,
// error) shape. It lives here, not in scm/write, because scm/write must
// never import internal/kernel (kernel is its only permitted importer —
// an import the other way would be a cycle).
type leaseAdapter struct {
	store LeaseStore
}

func (a leaseAdapter) Acquire(ctx context.Context, resource, holder string, ttl time.Duration) (string, error) {
	lease, err := a.store.Acquire(ctx, resource, holder, ttl)
	if err != nil {
		return "", err
	}
	return lease.Token, nil
}

func (a leaseAdapter) Release(ctx context.Context, resource, token string) error {
	return a.store.Release(ctx, resource, token)
}

// PushBranch is the sole kernel-side entry point for pushing a branch
// (Constitution C4): every caller — a future Activities method, this
// task's own `make e2e-github` fixture, or any other kernel code —  goes
// through this function rather than constructing an
// internal/scm/write.Pusher directly, so there is exactly one place in
// this package that assembles the LeaseAcquirer/Ledger adapters correctly.
func PushBranch(
	ctx context.Context,
	leases LeaseStore,
	ledger ExternalOpStore,
	tokens write.TokenSource,
	req write.PushRequest,
) (write.Receipt, error) {
	pusher := &write.Pusher{
		Leases: leaseAdapter{store: leases},
		Ledger: ledger,
		Tokens: tokens,
		Holder: "kernel",
	}
	return pusher.PushBranch(ctx, req)
}
