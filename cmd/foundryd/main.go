// Command foundryd is the Temporal worker process hosting the kernel's
// DeliverPlan workflow and its activities (docs/PLAN.md Task 12, SKP-10).
// It is the only process that ever performs the side effects the kernel
// owns (Constitution C4) — worktree mutation, executor invocation,
// evidence persistence, transition/lease/receipt storage.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/claudecode"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// taskQueue is the Temporal task queue this worker polls (docs/PLAN.md
// Task 12 Step 8).
const taskQueue = "foundry-core"

func main() {
	if err := run(); err != nil {
		log.Fatalf("foundryd: %v", err)
	}
}

func run() error {
	temporalHostPort := envOr("TEMPORAL_HOSTPORT", "temporal:7233")
	pgDSN := envOr("PG_DSN", "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable")
	keyDir := os.Getenv("FOUNDRY_KEY_DIR")
	worktreeRoot := envOr("FOUNDRY_WORKTREE_ROOT", "/var/lib/foundry/worktrees")
	evidenceRoot := envOr("FOUNDRY_EVIDENCE_ROOT", "/var/lib/foundry/evidence")

	if keyDir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("resolve key dir: %w", err)
		}
		keyDir = d
	}
	pub, err := provenance.LoadPublicKey(keyDir)
	if err != nil {
		return fmt.Errorf("load approver public key: %w", err)
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()

	rawStore, err := provenance.OpenPGRawStore(pgDSN)
	if err != nil {
		return fmt.Errorf("open provenance store: %w", err)
	}
	defer rawStore.Close()

	activities := kernel.NewActivities(
		provenance.NewStore(rawStore, pub),
		&worktree.Manager{Root: worktreeRoot},
		evidence.NewFSStore(evidenceRoot),
		kernel.NewPGLeaseStore(db),
		kernel.NewPGReceiptStore(db),
		kernel.NewPGTransitionStore(db),
	)

	c, err := client.Dial(client.Options{HostPort: temporalHostPort})
	if err != nil {
		return fmt.Errorf("dial temporal at %s: %w", temporalHostPort, err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(kernel.DeliverPlan)
	registerActivities(w, activities)

	// worker.InterruptCh returns a <-chan interface{} that closes on
	// SIGINT/SIGTERM, which is what w.Run needs for graceful shutdown
	// (docs/PLAN.md Task 12 Step 8).
	if err := w.Run(worker.InterruptCh()); err != nil {
		return fmt.Errorf("worker run: %w", err)
	}
	return nil
}

// registerActivities registers each Activities method under the name
// workflow.go's activity constants reference, so the workflow's
// ExecuteActivity-by-name calls resolve here.
func registerActivities(w worker.Worker, a *kernel.Activities) {
	w.RegisterActivityWithOptions(a.LoadApprovedPlan, activity.RegisterOptions{Name: kernel.ActivityLoadApprovedPlan})
	w.RegisterActivityWithOptions(a.AcquireLease, activity.RegisterOptions{Name: kernel.ActivityAcquireLease})
	w.RegisterActivityWithOptions(a.AcquireWorktree, activity.RegisterOptions{Name: kernel.ActivityAcquireWorktree})
	w.RegisterActivityWithOptions(a.ReleaseWorktree, activity.RegisterOptions{Name: kernel.ActivityReleaseWorktree})
	w.RegisterActivityWithOptions(a.ExecuteTask, activity.RegisterOptions{Name: kernel.ActivityExecuteTask})
	w.RegisterActivityWithOptions(a.ValidateTask, activity.RegisterOptions{Name: kernel.ActivityValidateTask})
	w.RegisterActivityWithOptions(a.RecordEvidence, activity.RegisterOptions{Name: kernel.ActivityRecordEvidence})
	w.RegisterActivityWithOptions(a.AppendTransition, activity.RegisterOptions{Name: kernel.ActivityAppendTransition})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
