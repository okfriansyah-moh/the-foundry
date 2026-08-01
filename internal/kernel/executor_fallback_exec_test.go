package kernel_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// faultAdapter's Run returns a typed provider fault; okAdapter's Run succeeds.
type faultAdapter struct{}

func (faultAdapter) Prepare(context.Context, worktree.Workspace, executor.TaskPacket) error {
	return nil
}

func (faultAdapter) Run(context.Context) (executor.Summary, error) {
	return executor.Summary{}, fmt.Errorf("simulated: %w", executor.ErrProviderUnavailable)
}

func (faultAdapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{}, nil
}

type okAdapter struct{}

func (okAdapter) Prepare(context.Context, worktree.Workspace, executor.TaskPacket) error { return nil }

func (okAdapter) Run(context.Context) (executor.Summary, error) {
	return executor.Summary{Claimed: "done"}, nil
}

func (okAdapter) Collect(context.Context) (executor.Artifacts, error) {
	return executor.Artifacts{}, nil
}

func init() {
	executor.Register("t129-fault", func() executor.Adapter { return faultAdapter{} })
	executor.Register("t129-ok", func() executor.Adapter { return okAdapter{} })
	executor.Register("t129-fault2", func() executor.Adapter { return faultAdapter{} })
}

func t129Registry() capability.Registry {
	return capability.Registry{Executors: []capability.Record{
		{Provider: "t129-fault", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
		{Provider: "t129-fault2", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
		{Provider: "t129-ok", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
	}}
}

// TestExecuteTask_FallsOverToNextAllowedProvider proves docs/PLAN.md Task 129: a
// task whose first-choice provider is unavailable completes on the next
// policy-allowed provider within the attempt budget, and the skipped provider is
// recorded on the output.
func TestExecuteTask_FallsOverToNextAllowedProvider(t *testing.T) {
	acts := &kernel.Activities{
		ReceiptStore:       kernel.NewMemReceiptStore(),
		CapabilityRegistry: t129Registry(),
		ExecutorHealth:     capability.NewHealthTracker(),
		ExecutorSelector: kernel.ExecutorSelector{
			Routing: kernel.RoutingTable{"backend": {"t129-fault", "t129-ok"}},
			Profile: "personal",
		},
	}
	out, err := acts.ExecuteTask(context.Background(), kernel.ExecuteTaskInput{
		WorkflowID:        "wf1",
		TaskID:            "t1",
		Attempt:           1,
		TaskClass:         "backend",
		ExecutorAllowlist: []string{"t129-fault", "t129-ok"},
		WorkspacePath:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ExecuteTask: %v", err)
	}
	if out.Failed {
		t.Fatalf("task should have completed on the fallback provider, got failed: %s", out.ErrorMessage)
	}
	if out.ExecutorUsed != "t129-ok" {
		t.Fatalf("ExecutorUsed = %q, want t129-ok (fell over from the unavailable first choice)", out.ExecutorUsed)
	}
	found := false
	for _, s := range out.SkippedExecutors {
		if s == "t129-fault" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped list %v must record the unavailable provider t129-fault", out.SkippedExecutors)
	}
}

// TestExecuteTask_HealthyOutsideAllowlistNeverSelected proves a healthy provider
// that is not in the allowlist is never tried, even when it is the only one that
// would succeed — the fallback can never escape policy (C4).
func TestExecuteTask_HealthyOutsideAllowlistNeverSelected(t *testing.T) {
	acts := &kernel.Activities{
		ReceiptStore:       kernel.NewMemReceiptStore(),
		CapabilityRegistry: t129Registry(),
		ExecutorHealth:     capability.NewHealthTracker(),
		ExecutorSelector: kernel.ExecutorSelector{
			Routing: kernel.RoutingTable{"backend": {"t129-fault", "t129-fault2"}},
			Profile: "personal",
		},
	}
	// t129-ok is healthy but NOT in the allowlist; both allowed providers fault.
	out, err := acts.ExecuteTask(context.Background(), kernel.ExecuteTaskInput{
		WorkflowID:        "wf1",
		TaskID:            "t1",
		Attempt:           1,
		TaskClass:         "backend",
		ExecutorAllowlist: []string{"t129-fault", "t129-fault2"},
		WorkspacePath:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ExecuteTask: %v", err)
	}
	if !out.Failed {
		t.Fatal("must fail closed when every allowed provider is unavailable")
	}
	if out.ExecutorUsed == "t129-ok" {
		t.Fatal("a healthy provider outside the allowlist must never be selected")
	}
	if len(out.SkippedExecutors) == 0 {
		t.Fatal("fail-closed outcome must carry a diagnosable skip list")
	}
}
