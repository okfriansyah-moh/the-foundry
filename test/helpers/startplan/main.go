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

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// taskQueue must match cmd/foundryd/main.go's taskQueue constant — the
// worker this tool's started workflow is dispatched to.
const taskQueue = "foundry-core"

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
	flag.Parse()

	if *workflowID == "" || *planID == "" || *planFile == "" || *repoPath == "" {
		return fmt.Errorf("usage: startplan --workflow-id ID --plan-id ID --plan-file PATH --repo-path PATH [--executor NAME] [--temporal-hostport HOST:PORT]")
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
		PlanID:       *planID,
		PlanFilePath: *planFile,
		RepoPath:     *repoPath,
		ExecutorName: *executorName,
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
