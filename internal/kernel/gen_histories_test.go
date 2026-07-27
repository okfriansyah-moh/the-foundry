package kernel_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// TestGenerateHistories is not part of the normal test suite — it is a
// one-time (re-runnable) fixture generator for replay_test.go's recorded
// histories under test/histories/. It requires the Temporal CLI dev-server
// binary (downloaded on first use by go.temporal.io/sdk/testsuite, which
// needs network access) — it is gated behind KERNEL_GEN_HISTORIES=1 so
// normal `go test ./internal/kernel/...` runs never depend on downloading
// or spawning that binary. Run: KERNEL_GEN_HISTORIES=1 go test
// ./internal/kernel/ -run TestGenerateHistories -v.
func TestGenerateHistories(t *testing.T) {
	if os.Getenv("KERNEL_GEN_HISTORIES") == "" {
		t.Skip("set KERNEL_GEN_HISTORIES=1 to (re)generate test/histories/ fixtures")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	srv, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{})
	if err != nil {
		t.Fatalf("start dev server: %v", err)
	}
	defer srv.Stop()

	historiesDir := filepath.Join("..", "..", "test", "histories")
	if err := os.MkdirAll(historiesDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", historiesDir, err)
	}

	generateOne(t, ctx, srv.Client(), scriptSuccess, filepath.Join(historiesDir, "hello_world.json"))
	generateOne(t, ctx, srv.Client(), scriptFailure, filepath.Join(historiesDir, "failing_task.json"))
}

func generateOne(t *testing.T, ctx context.Context, c client.Client, script, outPath string) {
	t.Helper()
	fx := newFixture(t, script)

	taskQueue := "gen-histories-" + t.Name()
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(kernel.DeliverPlan)
	registerWorkerActivities(w, fx.Activities)
	if err := w.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	defer w.Stop()

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{TaskQueue: taskQueue}, kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:       fx.PlanID,
		PlanFilePath: fx.PlanFilePath,
		RepoPath:     fx.RepoPath,
		ExecutorName: "fake",
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	var result kernel.DeliverPlanResult
	_ = run.Get(ctx, &result) // ignore the returned business error; we still want its history.

	hist := &historypb.History{}
	iter := c.GetWorkflowHistory(ctx, run.GetID(), run.GetRunID(), false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iter.HasNext() {
		ev, err := iter.Next()
		if err != nil {
			t.Fatalf("read history event: %v", err)
		}
		hist.Events = append(hist.Events, ev)
	}

	raw, err := protojson.Marshal(hist)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if err := os.WriteFile(outPath, raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", outPath, err)
	}
	t.Logf("wrote %s (%d events)", outPath, len(hist.Events))
}

// registerWorkerActivities mirrors registerActivities but against a real
// worker.Worker (used only by the generator, which needs a live server
// connection rather than a TestWorkflowEnvironment).
func registerWorkerActivities(w worker.Worker, a *kernel.Activities) {
	w.RegisterActivityWithOptions(a.LoadApprovedPlan, activity.RegisterOptions{Name: kernel.ActivityLoadApprovedPlan})
	w.RegisterActivityWithOptions(a.RecheckApproval, activity.RegisterOptions{Name: kernel.ActivityRecheckApproval})
	w.RegisterActivityWithOptions(a.ReserveBudget, activity.RegisterOptions{Name: kernel.ActivityReserveBudget})
	w.RegisterActivityWithOptions(a.AcquireLease, activity.RegisterOptions{Name: kernel.ActivityAcquireLease})
	w.RegisterActivityWithOptions(a.AcquireWorktree, activity.RegisterOptions{Name: kernel.ActivityAcquireWorktree})
	w.RegisterActivityWithOptions(a.ReleaseWorktree, activity.RegisterOptions{Name: kernel.ActivityReleaseWorktree})
	w.RegisterActivityWithOptions(a.ExecuteTask, activity.RegisterOptions{Name: kernel.ActivityExecuteTask})
	w.RegisterActivityWithOptions(a.ValidateTask, activity.RegisterOptions{Name: kernel.ActivityValidateTask})
	w.RegisterActivityWithOptions(a.RecordEvidence, activity.RegisterOptions{Name: kernel.ActivityRecordEvidence})
	w.RegisterActivityWithOptions(a.AppendTransition, activity.RegisterOptions{Name: kernel.ActivityAppendTransition})
}
