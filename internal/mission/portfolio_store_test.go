package mission_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// openPortfolioTestDB reuses the mission PG test bootstrap and additionally
// creates the portfolio + cost-ledger tables this suite needs, so it does not
// depend on `make migrate-up` having run first (same rationale as openTestDB).
func openPortfolioTestDB(t *testing.T) (*mission.PortfolioStore, *sql.DB) {
	t.Helper()
	db := openTestDB(t) // skips when no DSN configured
	const ddl = `
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
);
CREATE TABLE IF NOT EXISTS budgets (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    scope_id     TEXT NOT NULL,
    kind         TEXT NOT NULL,
    period       TEXT NOT NULL,
    ceiling_usd  NUMERIC(12, 4) NOT NULL,
    reserved_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    incurred_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, scope_id, kind, period)
);
CREATE TABLE IF NOT EXISTS cost_entries (
    id             TEXT PRIMARY KEY,
    scope          TEXT NOT NULL,
    scope_id       TEXT NOT NULL,
    state          TEXT NOT NULL,
    amount_usd     NUMERIC(12, 4) NOT NULL,
    pricing_version TEXT NOT NULL,
    provider       TEXT NOT NULL,
    meta           JSONB,
    budget_id      TEXT,
    at             TIMESTAMPTZ NOT NULL DEFAULT now()
);`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("create portfolio schema: %v", err)
	}
	return mission.NewPortfolioStore(db), db
}

func seedPortfolio(t *testing.T, s *mission.PortfolioStore, id string, cap int, missions ...mission.PortfolioMission) {
	t.Helper()
	ctx := context.Background()
	if err := s.EnsurePortfolio(ctx, id, cap); err != nil {
		t.Fatalf("ensure portfolio: %v", err)
	}
	for _, m := range missions {
		if err := s.UpsertMission(ctx, id, m); err != nil {
			t.Fatalf("upsert mission %q: %v", m.ID, err)
		}
	}
}

// TestPortfolioStoreCapFailsClosed proves the active-mission cap holds against
// the persisted state: a third activation past a cap of 2 fails closed.
func TestPortfolioStoreCapFailsClosed(t *testing.T) {
	s, _ := openPortfolioTestDB(t)
	ctx := context.Background()
	pid := "pf-cap-" + randSuffix(t)
	seedPortfolio(t, s, pid, 2,
		mission.PortfolioMission{ID: pid + "-a", MonthlyBudgetUSD: 100},
		mission.PortfolioMission{ID: pid + "-b", MonthlyBudgetUSD: 100},
		mission.PortfolioMission{ID: pid + "-c", MonthlyBudgetUSD: 100},
	)
	if err := s.Activate(ctx, pid, pid+"-a"); err != nil {
		t.Fatalf("activate a: %v", err)
	}
	if err := s.Activate(ctx, pid, pid+"-b"); err != nil {
		t.Fatalf("activate b: %v", err)
	}
	if err := s.Activate(ctx, pid, pid+"-c"); !errors.Is(err, mission.ErrPortfolioCapReached) {
		t.Fatalf("activate c past cap: want ErrPortfolioCapReached, got %v", err)
	}
}

// TestPortfolioStoreChargeIsolation proves a charge to one mission provably
// cannot touch another's persisted envelope.
func TestPortfolioStoreChargeIsolation(t *testing.T) {
	s, _ := openPortfolioTestDB(t)
	ctx := context.Background()
	pid := "pf-iso-" + randSuffix(t)
	seedPortfolio(t, s, pid, 0,
		mission.PortfolioMission{ID: pid + "-a", MonthlyBudgetUSD: 100, Active: true},
		mission.PortfolioMission{ID: pid + "-b", MonthlyBudgetUSD: 100, Active: true},
	)
	// Exhaust A completely.
	if err := s.Charge(ctx, pid, pid+"-a", 100); err != nil {
		t.Fatalf("charge a: %v", err)
	}
	if err := s.Charge(ctx, pid, pid+"-a", 1); !errors.Is(err, mission.ErrPortfolioOverBudget) {
		t.Fatalf("over-budget a: want ErrPortfolioOverBudget, got %v", err)
	}
	// B is untouched: it can still spend its full envelope.
	if err := s.Charge(ctx, pid, pid+"-b", 100); err != nil {
		t.Fatalf("charge b after a exhausted: %v", err)
	}
	loaded, err := s.Load(ctx, pid)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.Remaining(pid + "-b"); got != 0 {
		t.Fatalf("b remaining after full spend: want 0, got %v", got)
	}
}

// TestPortfolioStoreFairnessSurvivesReload proves the fair-schedule counter is
// persisted, so the fairness spread bound holds across a reload (the in-process
// analogue of a restart).
func TestPortfolioStoreFairnessSurvivesReload(t *testing.T) {
	s, _ := openPortfolioTestDB(t)
	ctx := context.Background()
	pid := "pf-fair-" + randSuffix(t)
	seedPortfolio(t, s, pid, 0,
		mission.PortfolioMission{ID: pid + "-a", Active: true},
		mission.PortfolioMission{ID: pid + "-b", Active: true},
		mission.PortfolioMission{ID: pid + "-c", Active: true},
	)
	now := time.Now()
	for i := 0; i < 9; i++ {
		if _, ok, err := s.NextScheduled(ctx, pid, now); err != nil || !ok {
			t.Fatalf("schedule tick %d: ok=%v err=%v", i, ok, err)
		}
	}
	// Reload from Postgres (simulating a fresh process) and assert the spread
	// bound still holds — the counters were durable, not process-local.
	reloaded, err := s.Load(ctx, pid)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if spread := reloaded.FairnessSpread(); spread > 1 {
		t.Fatalf("fairness spread after reload: want <=1, got %d", spread)
	}
	// Each of three active missions picked exactly three of nine turns.
	for _, r := range reloaded.Panel() {
		if r.Scheduled != 3 {
			t.Fatalf("mission %q scheduled %d after reload, want 3", r.ID, r.Scheduled)
		}
	}
}

// TestPortfolioStoreActivatePendingUpToCap proves the supervisor admission step
// never exceeds the cap and is deterministic.
func TestPortfolioStoreActivatePendingUpToCap(t *testing.T) {
	s, _ := openPortfolioTestDB(t)
	ctx := context.Background()
	pid := "pf-admit-" + randSuffix(t)
	seedPortfolio(t, s, pid, 2,
		mission.PortfolioMission{ID: pid + "-a"},
		mission.PortfolioMission{ID: pid + "-b"},
		mission.PortfolioMission{ID: pid + "-c"},
	)
	activated, err := s.ActivatePendingUpToCap(ctx, pid)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if len(activated) != 2 {
		t.Fatalf("admit: activated %v, want exactly 2 (cap)", activated)
	}
	ids, err := s.ActiveMissionIDs(ctx, pid)
	if err != nil {
		t.Fatalf("active ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("active count %d after admit, want 2", len(ids))
	}
	// A second admit is a no-op: the portfolio is already at cap.
	again, err := s.ActivatePendingUpToCap(ctx, pid)
	if err != nil {
		t.Fatalf("second admit: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second admit activated %v, want none", again)
	}
}

// TestPortfolioBudgetIsolationLedger proves the isolation Task 81's acceptance
// actually claimed: against the COST LEDGER (cost_entries/budgets), a charge to
// mission A's envelope cannot reduce mission B's available envelope. This is a
// ledger query, not a struct assertion.
func TestPortfolioBudgetIsolationLedger(t *testing.T) {
	s, db := openPortfolioTestDB(t)
	_ = s
	costStore := cost.NewStore(db)
	ctx := context.Background()
	period := time.Now().Format("2006-01")
	a := "iso-a-" + randSuffix(t)
	b := "iso-b-" + randSuffix(t)

	if _, err := costStore.CreateBudget(ctx, cost.ScopeMission, a, cost.KindMissionMonthly, period, 100); err != nil {
		t.Fatalf("budget a: %v", err)
	}
	bBudget, err := costStore.CreateBudget(ctx, cost.ScopeMission, b, cost.KindMissionMonthly, period, 100)
	if err != nil {
		t.Fatalf("budget b: %v", err)
	}
	// Reserve heavily against A.
	if _, err := costStore.Reserve(ctx, cost.ScopeMission, a, cost.KindMissionMonthly, period, 90, "test", "v0", nil); err != nil {
		t.Fatalf("reserve a: %v", err)
	}
	// B's envelope is provably untouched: its available is still its full
	// ceiling minus its own (zero) reservations.
	after, err := costStore.GetBudget(ctx, cost.ScopeMission, b, cost.KindMissionMonthly, period)
	if err != nil {
		t.Fatalf("get budget b: %v", err)
	}
	if after.ReservedUSD != bBudget.ReservedUSD {
		t.Fatalf("mission B reserved changed by a charge to A: before=%v after=%v", bBudget.ReservedUSD, after.ReservedUSD)
	}
}
