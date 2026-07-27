package kernel_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// TestLaneWorkers_EachPollsItsOwnTaskQueue is docs/PLAN.md Task 96
// (FND-14R2)'s Acceptance-required "integration-style test": it starts
// one real Temporal worker per config/queue-priority.yaml lane — the
// same registration shape cmd/foundryd/main.go's run() now performs —
// against this repo's own docker-compose Temporal, then executes a
// trivial workflow through each lane's LaneSelector-resolved task queue.
// A config-only unit test (TestLaneSelector_Select above) cannot prove a
// worker "actually starts and polls" its queue; only a live round trip
// can.
//
// Gated behind RUN_LANE_LIVE=1, mirroring internal/recovery's own
// RUN_RECOVERY_LIVE=1 precedent (test/recovery_supervisor_live_test.go)
// because it needs a real TEMPORAL_HOSTPORT — never available by default
// in a bare `go test ./...` run outside `make test`'s Docker environment.
func TestLaneWorkers_EachPollsItsOwnTaskQueue(t *testing.T) {
	if os.Getenv("RUN_LANE_LIVE") == "" {
		t.Skip("set RUN_LANE_LIVE=1 to run against a live Temporal (see test/recovery_supervisor_live_test.go)")
	}

	hostPort := envOrLane("TEMPORAL_HOSTPORT", "temporal:7233")
	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		t.Fatalf("dial temporal at %s: %v", hostPort, err)
	}
	defer c.Close()

	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig(%s): %v", repoQueueConfigPath, err)
	}

	var workers []worker.Worker
	defer func() {
		for _, w := range workers {
			w.Stop()
		}
	}()

	// One worker per lane, exactly as cmd/foundryd/main.go registers —
	// pingWorkflow stands in for kernel.DeliverPlan (whose own decision
	// graph is out of this task's scope and already replay-tested
	// elsewhere); this test only proves lane->queue routing.
	for _, lane := range cfg.Lanes {
		w := worker.New(c, lane.TaskQueue, worker.Options{})
		w.RegisterWorkflow(pingWorkflow)
		if err := w.Start(); err != nil {
			t.Fatalf("start worker for lane %q (task queue %q): %v", lane.Name, lane.TaskQueue, err)
		}
		workers = append(workers, w)
	}

	// Give each poller a moment to register with the Temporal server
	// before dispatching workflows at it.
	time.Sleep(500 * time.Millisecond)

	var sel kernel.LaneSelector
	for _, lane := range cfg.Lanes {
		taskQueue, err := sel.Select(lane.Name, cfg)
		if err != nil {
			t.Fatalf("LaneSelector.Select(%q): %v", lane.Name, err)
		}
		if taskQueue != lane.TaskQueue {
			t.Fatalf("LaneSelector.Select(%q) = %q, want %q", lane.Name, taskQueue, lane.TaskQueue)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:        fmt.Sprintf("lane-live-%s-%d", lane.Name, time.Now().UnixNano()),
			TaskQueue: taskQueue,
		}, pingWorkflow)
		if err != nil {
			cancel()
			t.Fatalf("start workflow on lane %q (queue %q): %v", lane.Name, taskQueue, err)
		}
		var result string
		getErr := run.Get(ctx, &result)
		cancel()
		if getErr != nil {
			t.Fatalf("lane %q (queue %q): workflow did not complete — its worker did not poll the queue: %v", lane.Name, taskQueue, getErr)
		}
		if result != "pong" {
			t.Errorf("lane %q: result = %q, want %q", lane.Name, result, "pong")
		}
	}
}

// pingWorkflow is a minimal, deterministic stand-in workflow used only to
// prove a given lane's worker actually polls its assigned task queue.
func pingWorkflow(ctx workflow.Context) (string, error) {
	return "pong", nil
}

func envOrLane(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
