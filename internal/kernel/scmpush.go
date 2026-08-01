package kernel

import (
	"context"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/scm/write"
)

// scmpush.go is the one file in this repository permitted to import
// internal/scm/write (Constitution C4; docs/PLAN.md Task 27/FND-08). Task
// 28's fitlint authority rule enforces this compile-time (Constitution C4).
//
// PushBranch below is the sole permitted internal/scm/write call site
// (Constitution C4). Task 108's TenXDeliver workflow reaches it through
// Activities.IntegrateChangeSet after SelectBranchDeliveryPolicy resolves
// the org policy (docs/foundry/docs/workflows/multi-repository.md §N10.2).
// DeliverPlan's per-task loop does not call it directly — venture delivery
// uses a different terminal path.

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
