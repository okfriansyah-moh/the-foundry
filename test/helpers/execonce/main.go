// Command execonce calls internal/kernel.Activities.ExecuteTask exactly
// once, backed by a real Postgres receipts table (internal/kernel's
// PGReceiptStore), and exits nonzero if the call errors. It exists so
// test/skp_resume_test.sh can prove — against a real database, not an
// in-memory stand-in — that internal/kernel's idempotency receipt (Task
// 12's ReceiptStore) is what makes a repeated call to the same
// (workflow, task, attempt) key a no-op, and that deleting that receipt
// row makes the same call genuinely re-invoke the executor adapter
// (docs/PLAN.md Task 16 / SKP-14, Step 4's negative control).
//
// It talks to no Temporal server at all — ExecuteTask is an ordinary Go
// method, callable directly outside any workflow, which is exactly what
// this negative control needs: a way to invoke the same activity twice
// with the receipt row deleted in between, without needing to force a
// Temporal-level activity retry to land in an exact timing window.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("execonce: %v", err)
	}
}

func run() error {
	pgDSN := flag.String("pg-dsn", "", "Postgres DSN (required)")
	workflowID := flag.String("workflow-id", "", "idempotency key: workflow ID (required)")
	taskID := flag.String("task-id", "", "idempotency key: task ID (required)")
	attempt := flag.Int("attempt", 1, "idempotency key: logical attempt")
	scriptPath := flag.String("script", "", "fake_script.yaml path (required)")
	workspace := flag.String("workspace", "", "scratch workspace directory (required)")
	flag.Parse()

	if *pgDSN == "" || *workflowID == "" || *taskID == "" || *scriptPath == "" || *workspace == "" {
		return fmt.Errorf("usage: execonce --pg-dsn DSN --workflow-id ID --task-id ID --script PATH --workspace DIR [--attempt N]")
	}

	if err := os.MkdirAll(*workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace %s: %w", *workspace, err)
	}

	db, err := sql.Open("pgx", *pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Only ReceiptStore matters: ExecuteTask never touches the other
	// Activities collaborators (provenance, worktree, evidence, lease,
	// transitions, cost store/defaults, validator). Task 116 (SEC-02):
	// ExecuteTask fails closed without an executor allowlist, so wire the
	// deterministic selector + a capability registry supporting "fake".
	activities := kernel.NewActivities(nil, nil, nil, nil, kernel.NewPGReceiptStore(db), nil, nil, cost.Defaults{}, verify.Runner{})
	activities.ExecutorSelector = kernel.ExecutorSelector{Default: "fake"}
	activities.CapabilityRegistry = capability.Registry{Executors: []capability.Record{
		{Provider: "fake", ExecutionClass: "test", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
	}}

	out, err := activities.ExecuteTask(context.Background(), kernel.ExecuteTaskInput{
		WorkflowID:        *workflowID,
		TaskID:            *taskID,
		Attempt:           *attempt,
		ExecutorName:      "fake",
		WorkspacePath:     *workspace,
		ExecutorAllowlist: []string{"fake"},
		Packet:            executor.TaskPacket{Goal: *scriptPath},
	})
	if err != nil {
		return fmt.Errorf("execute task: %w", err)
	}
	if out.Failed {
		return fmt.Errorf("execute task reported failure: %s", out.ErrorMessage)
	}

	fmt.Printf("execonce: OK claimed=%q\n", out.Claimed)
	return nil
}
