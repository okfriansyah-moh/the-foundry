// Command startplan starts one DeliverPlan workflow run against a live
// Temporal server and returns immediately (it does not wait for the run to
// finish) — a test-only tool for test/skp_resume_test.sh (docs/PLAN.md Task
// 16 / SKP-14).
//
// No CLI or programmatic entry point to start DeliverPlan against a real
// Temporal server exists anywhere else in the repo yet — Task 12's own
// planned live e2e path (`go run ./test/e2e/skp_basic`, referenced in its
// PLAN.md Status line) was never built either, for the same "no Docker
// daemon in this environment" reason recorded on Tasks 2/4/8/12/13/14/15.
// Starting a workflow is a plain Temporal client call, not a kernel side
// effect itself (every actual side effect still lives in cmd/foundryd's
// kernel.Activities, per Constitution C4), so this is kept here under
// test/ as the smallest reversible addition needed to make
// skp_resume_test.sh runnable, rather than growing cmd/foundry's
// production CLI surface for a need only this test has today.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// The dead task-queue constant Task 96 orphaned is gone: this helper now
// resolves its Temporal task queue through the kernel's LaneSelector against
// config/queue-priority.yaml, exactly as the production trigger
// kernel.StartDelivery does (docs/PLAN.md Task 105 / RTC-01). The production
// single-edge from an ApprovedPlan to a running DeliverPlan is
// kernel.StartDelivery (behind POST /v1/plans/{id}/deliver and
// `foundry plan run`); this test helper deliberately keeps a caller-supplied
// workflow ID for test/skp_e2e.sh / skp_resume_test.sh, which key their own
// DB/Temporal assertions off that fixed ID, while sharing the kernel's lane
// resolution so it never targets an unpolled queue again.

func main() {
	if err := run(); err != nil {
		log.Fatalf("startplan: %v", err)
	}
}

func run() error {
	hostPort := flag.String("temporal-hostport", envOr("TEMPORAL_HOSTPORT", "temporal:7233"), "Temporal frontend host:port")
	workflowID := flag.String("workflow-id", "", "workflow ID to start (required)")
	planID := flag.String("plan-id", "", "approved plan ID (required)")
	planFile := flag.String("plan-file", "", "path to the plan.md file (required)")
	repoPath := flag.String("repo-path", "", "path to the repo DeliverPlan operates on (required)")
	executorName := flag.String("executor", "fake", "executor adapter name")
	executorAllowlist := flag.String("executor-allowlist", "", "comma-separated resolved executor_allowlist; when set, the kernel runs policy-checked selection (docs/PLAN.md Task 85)")
	lane := flag.String("lane", "", "queue-priority lane to resolve the task queue from (empty => delivery default)")
	queueConfig := flag.String("queue-config", envOr("FOUNDRY_QUEUE_PRIORITY", "config/queue-priority.yaml"), "path to config/queue-priority.yaml")
	flag.Parse()

	if *workflowID == "" || *planID == "" || *planFile == "" || *repoPath == "" {
		return fmt.Errorf("usage: startplan --workflow-id ID --plan-id ID --plan-file PATH --repo-path PATH [--executor NAME] [--executor-allowlist a,b] [--lane LANE] [--temporal-hostport HOST:PORT]")
	}

	// Resolve the task queue through the same kernel-owned LaneSelector the
	// production trigger uses (Constitution C4) — never a hardcoded queue.
	queueCfg, err := observe.LoadQueueConfig(*queueConfig)
	if err != nil {
		return fmt.Errorf("load queue config %s: %w", *queueConfig, err)
	}
	taskQueue, err := kernel.LaneSelector{}.Select(*lane, queueCfg)
	if err != nil {
		return fmt.Errorf("resolve lane %q: %w", *lane, err)
	}

	var allowlist []string
	if strings.TrimSpace(*executorAllowlist) != "" {
		for _, e := range strings.Split(*executorAllowlist, ",") {
			if e = strings.TrimSpace(e); e != "" {
				allowlist = append(allowlist, e)
			}
		}
	}

	c, err := client.Dial(client.Options{HostPort: *hostPort})
	if err != nil {
		return fmt.Errorf("dial temporal at %s: %w", *hostPort, err)
	}
	defer c.Close()

	wfRun, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        *workflowID,
		TaskQueue: taskQueue,
	}, kernel.DeliverPlan, kernel.DeliverPlanInput{
		PlanID:            *planID,
		PlanFilePath:      *planFile,
		RepoPath:          *repoPath,
		ExecutorName:      *executorName,
		ExecutorAllowlist: allowlist,
	})
	if err != nil {
		return fmt.Errorf("start workflow %s: %w", *workflowID, err)
	}

	fmt.Printf("started workflow_id=%s run_id=%s\n", *workflowID, wfRun.GetRunID())
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
