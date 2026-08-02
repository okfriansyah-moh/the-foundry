package kernel_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// docs/PLAN.md Task 105 (RTC-01) gated live proof. Gated behind
// RUN_START_LIVE=1 (mirrors RUN_RECOVERY_LIVE / RUN_GITHUB), requires a real
// TEMPORAL_HOSTPORT. It proves the single production edge against a real
// Temporal server: StartDelivery starts a DeliverPlan execution on the
// lane-resolved task queue, and starting the same plan twice collapses to one
// execution (ErrStartDuplicate) rather than three.
func TestStartDeliveryLive(t *testing.T) {
	if os.Getenv("RUN_START_LIVE") != "1" {
		t.Skip("set RUN_START_LIVE=1 (and TEMPORAL_HOSTPORT) to run the live start proof")
	}
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		hostPort = "temporal:7233"
	}
	tc, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		t.Fatalf("dial temporal at %s: %v", hostPort, err)
	}
	defer tc.Close()

	store, _, digest := newApprovedPlanStore(t)
	deps := kernel.StartDeps{
		Starter:           tc,
		Provenance:        store,
		QueueConfig:       loadQueueCfg(t),
		LaneSelector:      kernel.LaneSelector{},
		ExecutorAllowlist: []string{"fake"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := kernel.StartDelivery(ctx, deps, kernel.StartDeliveryInput{PlanID: "plan-start-1"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if out.WorkflowID != kernel.DeliveryWorkflowID(digest, 0, "") {
		t.Fatalf("non-deterministic workflow id: %s", out.WorkflowID)
	}
	if out.RunID == "" {
		t.Fatalf("expected a run id from the real Temporal start")
	}

	// A second identical start must collapse to one execution.
	if _, err := kernel.StartDelivery(ctx, deps, kernel.StartDeliveryInput{PlanID: "plan-start-1"}); !errors.Is(err, kernel.ErrStartDuplicate) {
		t.Fatalf("second start must be ErrStartDuplicate, got %v", err)
	}
}
