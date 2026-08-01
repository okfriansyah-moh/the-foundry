// docs/PLAN.md Task 121 (MMR-01) gated live restart proof. Gated at runtime by
// RUN_PORTFOLIO_LIVE=1 + PG_DSN + TEMPORAL_HOSTPORT, so a bare `go test ./...`
// never requires infra. Against the compose Temporal+Postgres it proves that
// after a supervisor worker is killed and restarted, the persisted activation,
// spend and fair-schedule state are unchanged, no mission is double-activated,
// and the fairness spread bound still holds -- the invariants that were
// fail-OPEN across a restart while portfolio state lived only in memory.
package recoverylive_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

const portfolioLiveDDL = `
CREATE TABLE IF NOT EXISTS portfolio_state (
    portfolio_id        TEXT PRIMARY KEY,
    max_active_products INTEGER NOT NULL DEFAULT 0,
    version             BIGINT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS portfolio_schedule (
    portfolio_id       TEXT NOT NULL REFERENCES portfolio_state (portfolio_id) ON DELETE CASCADE,
    mission_id         TEXT NOT NULL,
    active             BOOLEAN NOT NULL DEFAULT false,
    revenue_bearing    BOOLEAN NOT NULL DEFAULT false,
    monthly_budget_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    spent_usd          DOUBLE PRECISION NOT NULL DEFAULT 0,
    scheduled          BIGINT NOT NULL DEFAULT 0,
    last_scheduled_at  TIMESTAMPTZ,
    budget_scope       TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (portfolio_id, mission_id)
);`

func TestPortfolioRestartLive(t *testing.T) {
	if os.Getenv("RUN_PORTFOLIO_LIVE") != "1" {
		t.Skip("set RUN_PORTFOLIO_LIVE=1 (with PG_DSN + TEMPORAL_HOSTPORT) to run the live portfolio restart proof")
	}
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		t.Skip("PG_DSN not set")
	}
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		hostPort = "temporal:7233"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, portfolioLiveDDL); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	store := mission.NewPortfolioStore(db)
	acts := &mission.PortfolioActivities{Portfolio: store}
	pid := "pf-live-" + time.Now().Format("150405")

	if err := store.EnsurePortfolio(ctx, pid, 2); err != nil {
		t.Fatalf("ensure portfolio: %v", err)
	}
	for _, id := range []string{pid + "-a", pid + "-b", pid + "-c"} {
		if err := store.UpsertMission(ctx, pid, mission.PortfolioMission{ID: id, MonthlyBudgetUSD: 100}); err != nil {
			t.Fatalf("upsert %q: %v", id, err)
		}
	}

	tc, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		t.Fatalf("dial temporal: %v", err)
	}
	defer tc.Close()

	// --- first supervisor generation ---
	startSupervisor := func() worker.Worker {
		w := worker.New(tc, mission.PortfolioTaskQueue, worker.Options{})
		w.RegisterWorkflow(mission.PortfolioLoop)
		w.RegisterActivityWithOptions(acts.ReconcilePortfolio, activity.RegisterOptions{Name: mission.ActivityPortfolioReconcile})
		if err := w.Start(); err != nil {
			t.Fatalf("start worker: %v", err)
		}
		return w
	}

	w1 := startSupervisor()
	if _, err := tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        mission.PortfolioWorkflowID(pid),
		TaskQueue: mission.PortfolioTaskQueue,
	}, mission.PortfolioLoop, mission.PortfolioLoopInput{
		PortfolioID:    pid,
		CadenceSeconds: 1,
		MaxIterations:  1000,
	}); err != nil {
		t.Fatalf("start supervisor workflow: %v", err)
	}

	// Let the supervisor reconcile: it should activate exactly the cap (2) and
	// advance the fair schedule.
	waitForActive(t, store, pid, 2, 15*time.Second)
	if err := store.Charge(ctx, pid, pid+"-a", 42); err != nil {
		t.Fatalf("charge a: %v", err)
	}
	before, err := store.Load(ctx, pid)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}

	// --- simulate kill -9: stop the worker abruptly ---
	w1.Stop()
	time.Sleep(2 * time.Second)

	// --- restart: a fresh supervisor generation re-adopts the state ---
	w2 := startSupervisor()
	defer w2.Stop()
	// Re-issue the start; the running/again workflow must not double-activate.
	_, _ = tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        mission.PortfolioWorkflowID(pid),
		TaskQueue: mission.PortfolioTaskQueue,
	}, mission.PortfolioLoop, mission.PortfolioLoopInput{PortfolioID: pid, CadenceSeconds: 1, MaxIterations: 1000})
	time.Sleep(5 * time.Second)

	after, err := store.Load(ctx, pid)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}

	// Activation preserved and cap not exceeded (no double-activation).
	if after.ActiveCount() != 2 {
		t.Fatalf("active count after restart = %d, want 2 (cap preserved, no double-activation)", after.ActiveCount())
	}
	// Spend preserved exactly.
	if got := 100.0 - after.Remaining(pid+"-a"); got != 42 {
		t.Fatalf("spend on a after restart = %v, want 42 (preserved)", got)
	}
	// Fairness spread bound holds across the restart.
	if s := after.FairnessSpread(); s > 1 {
		t.Fatalf("fairness spread after restart = %d, want <=1", s)
	}
	// Schedule counters are monotonic across the restart (never reset to zero).
	beforeTotal, afterTotal := totalScheduled(before), totalScheduled(after)
	if afterTotal < beforeTotal {
		t.Fatalf("schedule counter reset across restart: before=%d after=%d", beforeTotal, afterTotal)
	}
}

func waitForActive(t *testing.T, store *mission.PortfolioStore, pid string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p, err := store.Load(context.Background(), pid)
		if err == nil && p.ActiveCount() == want {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("portfolio did not reach %d active missions within %s", want, timeout)
}

func totalScheduled(p *mission.Portfolio) int {
	total := 0
	for _, r := range p.Panel() {
		total += r.Scheduled
	}
	return total
}
