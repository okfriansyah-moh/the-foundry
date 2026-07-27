package cost_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
)

// testDSN returns the DSN to run real-Postgres cost-ledger tests against,
// or "" if none is configured. COST_TEST_PG_DSN is checked first (matches
// internal/ledger/extops's EXTOPS_TEST_PG_DSN precedent); PG_DSN is
// checked second so these tests run for free inside the dev container.
func testDSN() string {
	if v := os.Getenv("COST_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured, otherwise
// returns a live connection with budgets/cost_entries guaranteed to exist
// in their post-00009_budgets.sql shape, created here too so this test
// does not depend on `make migrate-up` having already run (same rationale
// as internal/ledger/extops/store_test.go's openTestDB).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("COST_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/ledger/cost/...` for a real Postgres")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	const ddl = `
CREATE TABLE IF NOT EXISTS cost_entries (
    id             TEXT PRIMARY KEY,
    scope          TEXT NOT NULL CHECK (scope IN ('workflow', 'product', 'mission')),
    scope_id       TEXT NOT NULL,
    state          TEXT NOT NULL CHECK (state IN ('reserved', 'estimated', 'incurred', 'reconciled', 'released', 'shadow')),
    amount_usd     NUMERIC(12, 4) NOT NULL,
    pricing_version TEXT NOT NULL,
    provider       TEXT NOT NULL,
    meta           JSONB,
    at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS budgets (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL CHECK (scope IN ('workflow', 'product', 'mission')),
    scope_id     TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('mission_monthly', 'provider', 'infra', 'experiment', 'reserve')),
    period       TEXT NOT NULL,
    ceiling_usd  NUMERIC(12, 4) NOT NULL,
    reserved_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    incurred_usd NUMERIC(12, 4) NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scope, scope_id, kind, period)
);
ALTER TABLE cost_entries ADD COLUMN IF NOT EXISTS budget_id TEXT REFERENCES budgets (id);
`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// randSuffix returns a short random hex suffix so repeated test runs
// (`-count=5`) against a persistent database never collide on the unique
// (scope, scope_id, kind, period) envelope key.
func randSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf)
}

func TestReserve_SingleReservationWithinCeiling(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}

	entry, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 4.00, "openai", "v1", map[string]string{"task_id": "t1"})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if entry.State != cost.StateReserved {
		t.Fatalf("state = %q, want reserved", entry.State)
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.ReservedUSD != 4.00 {
		t.Fatalf("reserved_usd = %v, want 4.00", budget.ReservedUSD)
	}
}

func TestReserve_ExhaustedCeilingRejected(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}

	if _, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 3.00, "openai", "v1", nil); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	_, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 3.00, "openai", "v1", nil)
	if !errors.Is(err, cost.ErrBudgetExhausted) {
		t.Fatalf("second reserve error = %v, want ErrBudgetExhausted", err)
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.ReservedUSD != 3.00 {
		t.Fatalf("reserved_usd = %v, want 3.00 (rejected reservation must not be applied)", budget.ReservedUSD)
	}
}

func TestReserve_NoEnvelopeReturnsNotFound(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	_, err := store.Reserve(ctx, cost.ScopeMission, "mission-"+randSuffix(t), cost.KindExperiment, "2026-07", 1.00, "openai", "v1", nil)
	if !errors.Is(err, cost.ErrBudgetNotFound) {
		t.Fatalf("error = %v, want ErrBudgetNotFound", err)
	}
}

func TestIncur_ReplacesReservedAmountWithActual(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}
	entry, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00, "openai", "v1", nil)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	updated, err := store.Incur(ctx, entry.ID, 3.50)
	if err != nil {
		t.Fatalf("incur: %v", err)
	}
	if updated.State != cost.StateIncurred || updated.AmountUSD != 3.50 {
		t.Fatalf("updated = %+v, want state=incurred amount=3.50", updated)
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.ReservedUSD != 0 {
		t.Fatalf("reserved_usd = %v, want 0 (moved to incurred)", budget.ReservedUSD)
	}
	if budget.IncurredUSD != 3.50 {
		t.Fatalf("incurred_usd = %v, want 3.50", budget.IncurredUSD)
	}

	// Incurring frees the headroom Reserve originally locked in, so a new
	// reservation for the difference must now succeed.
	if _, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 6.50, "openai", "v1", nil); err != nil {
		t.Fatalf("reserve after incur: %v", err)
	}
}

func TestIncur_RejectsNonReservedEntry(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}
	entry, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00, "openai", "v1", nil)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := store.Incur(ctx, entry.ID, 5.00); err != nil {
		t.Fatalf("first incur: %v", err)
	}
	if _, err := store.Incur(ctx, entry.ID, 5.00); !errors.Is(err, cost.ErrNotReserved) {
		t.Fatalf("second incur error = %v, want ErrNotReserved", err)
	}
}

func TestReconcile_DetectsDivergence(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}
	entry, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00, "openai", "v1", nil)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := store.Incur(ctx, entry.ID, 5.00); err != nil {
		t.Fatalf("incur: %v", err)
	}

	diverged, err := store.Reconcile(ctx, entry.ID, 5.75)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !diverged {
		t.Fatal("diverged = false, want true (5.00 incurred vs 5.75 observed)")
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.IncurredUSD != 5.75 {
		t.Fatalf("incurred_usd = %v, want 5.75 (reconciled to observed)", budget.IncurredUSD)
	}
}

func TestRelease_ReturnsReservationToEnvelope(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}
	entry, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00, "openai", "v1", nil)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	updated, err := store.Release(ctx, entry.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if updated.State != cost.StateReleased {
		t.Fatalf("state = %q, want released", updated.State)
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.ReservedUSD != 0 {
		t.Fatalf("reserved_usd = %v, want 0", budget.ReservedUSD)
	}

	// The full ceiling is available again for a new reservation.
	if _, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 5.00, "openai", "v1", nil); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
}

func TestRecordShadow_NoCeilingCheck(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	// No budget provisioned at all — RecordShadow must still succeed,
	// since subscription-priced entries have no ceiling to check.
	entry, err := store.RecordShadow(ctx, cost.ScopeWorkflow, "wf-"+randSuffix(t), 0.10, "claudecode", "v1", map[string]string{"task_id": "t1"})
	if err != nil {
		t.Fatalf("record shadow: %v", err)
	}
	if entry.State != cost.StateShadow {
		t.Fatalf("state = %q, want shadow", entry.State)
	}
	if entry.BudgetID != "" {
		t.Fatalf("budget_id = %q, want empty (shadow entries have no envelope)", entry.BudgetID)
	}
}

func TestRaiseCeiling_MonotonicallyIncreasing(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindMissionMonthly, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}

	raised, err := store.RaiseCeiling(ctx, cost.ScopeMission, scopeID, cost.KindMissionMonthly, "2026-07", 20.00)
	if err != nil {
		t.Fatalf("raise ceiling: %v", err)
	}
	if raised.CeilingUSD != 20.00 {
		t.Fatalf("ceiling_usd = %v, want 20.00", raised.CeilingUSD)
	}

	if _, err := store.RaiseCeiling(ctx, cost.ScopeMission, scopeID, cost.KindMissionMonthly, "2026-07", 15.00); !errors.Is(err, cost.ErrCeilingNotHigher) {
		t.Fatalf("lowering error = %v, want ErrCeilingNotHigher", err)
	}

	if _, err := store.RaiseCeiling(ctx, cost.ScopeMission, "no-such-scope", cost.KindMissionMonthly, "2026-07", 100); !errors.Is(err, cost.ErrBudgetNotFound) {
		t.Fatalf("unknown envelope error = %v, want ErrBudgetNotFound", err)
	}
}

func TestCreateBudget_DuplicateKeyRejected(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "mission-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindInfra, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindInfra, "2026-07", 20.00); !errors.Is(err, cost.ErrBudgetExists) {
		t.Fatalf("duplicate create error = %v, want ErrBudgetExists", err)
	}
}

// TestRaiseCeiling_RejectsNaNAndInfinity is a regression test for a real
// bypass found in secondary review (2026-07-26, Task 29): unlike IEEE 754
// float64, PostgreSQL's NUMERIC type accepts NaN and Infinity as legal
// values and orders both as greater than every finite value. Before this
// fix, RaiseCeiling's monotonic `ceiling_usd < $new` guard let a NaN/Inf
// ceiling through as "higher" than the current one (proven live against
// this same Postgres: any finite ceiling < 'NaN'::numeric is true), and
// once a budgets row's ceiling_usd was NaN/Inf, Reserve's
// `ceiling_usd - (reserved_usd + incurred_usd) >= $amount` WHERE clause
// became true for *any* finite amount — a $10 ceiling accepted a $999,999
// reservation. `foundry budget raise --ceiling NaN` (strconv.ParseFloat
// accepts "NaN"/"Inf" as valid input, cmd/foundry/budget.go) was a live,
// reachable path to this. RaiseCeiling/CreateBudget/Reserve/Incur/
// Reconcile/RecordShadow must all reject non-finite USD amounts outright.
func TestRaiseCeiling_RejectsNaNAndInfinity(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "nan-poc-" + randSuffix(t)
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 10.00); err != nil {
		t.Fatalf("create budget: %v", err)
	}

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := store.RaiseCeiling(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", bad); err == nil {
			t.Fatalf("RaiseCeiling(%v) = nil error, want rejection (non-finite ceiling must never be written)", bad)
		}
	}

	// The envelope must still be intact at its original ceiling — no
	// non-finite value snuck through — and a reservation within the real
	// ceiling still works, while one that would have exploited the bypass
	// (way beyond the $10 ceiling) is still rejected.
	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.CeilingUSD != 10.00 {
		t.Fatalf("ceiling_usd = %v, want unchanged 10.00 (a rejected RaiseCeiling must not mutate the row)", budget.CeilingUSD)
	}
	if _, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", 999999.0, "poc", "v1", nil); !errors.Is(err, cost.ErrBudgetExhausted) {
		t.Fatalf("Reserve($999999 against $10 ceiling) error = %v, want ErrBudgetExhausted (cap must still bind)", err)
	}

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", bad, "poc", "v1", nil); err == nil {
			t.Fatalf("Reserve(%v) = nil error, want rejection (non-finite amount must never be reserved)", bad)
		}
	}
	if _, err := store.CreateBudget(ctx, cost.ScopeMission, "nan-create-"+randSuffix(t), cost.KindExperiment, "2026-07", math.NaN()); err == nil {
		t.Fatal("CreateBudget(NaN ceiling) = nil error, want rejection")
	}
}

// TestReserve_ConcurrentNeverOversubscribes is the property test the task
// card's Acceptance bar requires: many goroutines race Reserve against a
// tight ceiling on a real Postgres, and the sum of successful
// reservations must never exceed that ceiling. Correctness here comes
// from Postgres's own row-level locking on the budgets UPDATE (see
// doc.go), not from any in-process mutex — sharing one *sql.DB (a real
// connection pool) across goroutines is what actually exercises that
// guarantee, matching how concurrent kernel activities would really hit
// this Store from independent Temporal worker goroutines/processes.
func TestReserve_ConcurrentNeverOversubscribes(t *testing.T) {
	db := openTestDB(t)
	store := cost.NewStore(db)
	ctx := context.Background()

	scopeID := "race-" + randSuffix(t)
	const ceiling = 10.00
	const amount = 1.00
	const workers = 50 // >> ceiling/amount, so exhaustion is guaranteed to bite.
	const wantSuccesses = int64(ceiling / amount)

	if _, err := store.CreateBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", ceiling); err != nil {
		t.Fatalf("create budget: %v", err)
	}

	var successes int64
	var unexpected int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07", amount, "openai", "v1", map[string]int{"worker": i})
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, cost.ErrBudgetExhausted):
				// expected once the ceiling is reached
			default:
				atomic.AddInt64(&unexpected, 1)
				t.Errorf("worker %d: unexpected error: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if unexpected != 0 {
		t.Fatalf("%d workers hit an unexpected error", unexpected)
	}
	if successes != wantSuccesses {
		t.Fatalf("successful reservations = %d, want exactly %d (ceiling %.2f / amount %.2f) — any other count means oversubscription or under-granting", successes, wantSuccesses, ceiling, amount)
	}

	budget, err := store.GetBudget(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, "2026-07")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if budget.ReservedUSD != ceiling {
		t.Fatalf("final reserved_usd = %v, want exactly %v (the ceiling, never more)", budget.ReservedUSD, ceiling)
	}
}
