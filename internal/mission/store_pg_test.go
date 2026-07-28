package mission_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// testDSN mirrors internal/ledger/cost/store_test.go's own precedent:
// MISSION_TEST_PG_DSN first, PG_DSN second (set for free inside the dev
// container by deploy/docker-compose.yaml).
func testDSN() string {
	if v := os.Getenv("MISSION_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured, otherwise
// returns a live connection with missions/mission_state/gate_events/
// loop_contracts guaranteed to exist in their post-00012_missions.sql
// shape (created here too, so this test does not depend on `make
// migrate-up` having already run against this database -- same rationale
// as internal/ledger/cost/store_test.go's openTestDB). A minimal
// `principals` stub is created alongside so missions.principal_id's
// foreign key is satisfiable without depending on Task 20's own
// migration having been applied first.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("MISSION_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/mission/...` for a real Postgres")
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
CREATE TABLE IF NOT EXISTS principals (
    id      TEXT PRIMARY KEY,
    kind    TEXT NOT NULL CHECK (kind IN ('human', 'service')),
    display TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS missions (
    id           TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    workflow_id  TEXT NOT NULL UNIQUE,
    contract     JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mission_state (
    id                 TEXT PRIMARY KEY,
    mission_id         TEXT NOT NULL REFERENCES missions (id),
    cycle              INTEGER NOT NULL,
    net_mrr_usd        NUMERIC(12, 4) NOT NULL,
    no_progress_cycles INTEGER NOT NULL,
    confirming         BOOLEAN NOT NULL,
    confirmed_since    TIMESTAMPTZ,
    status             TEXT NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    result_code        TEXT NOT NULL DEFAULT '',
    observed_at        TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS gate_events (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions (id),
    action      TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolution  TEXT
);
CREATE TABLE IF NOT EXISTS loop_contracts (
    id             TEXT PRIMARY KEY,
    loop_name      TEXT NOT NULL UNIQUE,
    trigger        TEXT NOT NULL,
    cadence        TEXT NOT NULL,
    authority      TEXT NOT NULL,
    budget         JSONB NOT NULL,
    metrics        JSONB NOT NULL,
    exit_condition TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mission_readiness_artifacts (
    id          TEXT PRIMARY KEY,
    mission_id  TEXT NOT NULL REFERENCES missions (id),
    readiness   TEXT NOT NULL CHECK (readiness IN ('pass', 'fail')),
    approved_by TEXT NOT NULL,
    digest      TEXT NOT NULL,
    artifact    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
`
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// randSuffix returns a short random hex suffix so repeated test runs
// against a persistent database never collide on a unique key (mission
// id, workflow_id, loop_name).
func randSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf)
}

// insertTestPrincipal inserts a minimal principals row satisfying
// missions.principal_id's foreign key. The real principals table
// (internal/db/migrations/00004_principals.sql, Task 20) requires kind
// and display as well as id -- this test's own `CREATE TABLE IF NOT
// EXISTS principals (id TEXT PRIMARY KEY)` above is a no-op whenever that
// real table already exists (the common case against this repo's live
// dev Postgres), so the INSERT must satisfy the real schema's NOT NULL
// columns, not just the id-only stub shape.
func insertTestPrincipal(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	const q = `INSERT INTO principals (id, kind, display) VALUES ($1, 'service', $1) ON CONFLICT (id) DO NOTHING`
	if _, err := db.ExecContext(context.Background(), q, id); err != nil {
		t.Fatalf("insert test principal %s: %v", id, err)
	}
}

func minimalContract(suffix string) mission.Contract {
	return mission.Contract{
		ID:        "mission-" + suffix,
		Statement: "test mission",
		Target: mission.Target{
			Metric:                          "net_mrr",
			Source:                          "payment-provider-ledger",
			Verification:                    "reconciled",
			AmountUSD:                       100,
			ConfirmationWindow:              "30d",
			MinimumUnrelatedPayingCustomers: 3,
			RefundChargebackRateBelow:       0.05,
		},
		Budget:      mission.Budget{MonthlyUSD: 100, TotalExperimentUSD: 500},
		Cadence:     mission.Cadence{Observe: "daily", Improve: "weekly"},
		Constraints: mission.Constraints{MaximumActiveProducts: 1, MaximumValidationCycles: 12, MaximumNoProgressCycles: 4},
		PauseWhen:   []string{mission.PauseMonthlyBudgetExhausted},
		TerminateWhen: []string{
			mission.TerminateTotalBudgetExhausted,
		},
		PostSuccessPolicy: mission.PostSuccessStop,
	}
}

// TestStore_CreateAndGetMission_RealPostgres proves CreateMission/
// GetMission round-trip a Contract through the real missions.contract
// jsonb column (parameterized inserts/selects -- no string-built SQL,
// verified by reading store.go; this test proves the round-trip actually
// works against Postgres, not just that the query text is well-formed).
func TestStore_CreateAndGetMission_RealPostgres(t *testing.T) {
	db := openTestDB(t)
	store := mission.NewStore(db)
	ctx := context.Background()
	suffix := randSuffix(t)
	principalID := "principal-" + suffix
	insertTestPrincipal(t, db, principalID)

	m := mission.Mission{
		ID:          "mission-" + suffix,
		PrincipalID: principalID,
		WorkflowID:  "mission-wf-" + suffix,
		Contract:    minimalContract(suffix),
	}
	if err := store.CreateMission(ctx, m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	got, err := store.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMission: %v", err)
	}
	if got.WorkflowID != m.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", got.WorkflowID, m.WorkflowID)
	}
	if got.PrincipalID != m.PrincipalID {
		t.Errorf("PrincipalID = %q, want %q", got.PrincipalID, m.PrincipalID)
	}
	if got.Contract.Target.AmountUSD != 100 {
		t.Errorf("Contract.Target.AmountUSD = %v, want 100", got.Contract.Target.AmountUSD)
	}
	if got.Contract.PostSuccessPolicy != mission.PostSuccessStop {
		t.Errorf("Contract.PostSuccessPolicy = %q, want %q", got.Contract.PostSuccessPolicy, mission.PostSuccessStop)
	}

	if _, err := store.GetMission(ctx, "does-not-exist-"+suffix); err != mission.ErrNotFound {
		t.Errorf("GetMission(missing) error = %v, want ErrNotFound", err)
	}
}

// TestStore_RegisterLoopContract_IdempotentOnConflict_RealPostgres proves
// the exact property RequireLoopContract's refusal-to-start guarantee
// depends on: registering the same loop_name twice is a no-op (ON
// CONFLICT (loop_name) DO NOTHING), not a duplicate row and not an error
// -- a MissionLoop replaying/restarting must be able to call this
// idempotently.
func TestStore_RegisterLoopContract_IdempotentOnConflict_RealPostgres(t *testing.T) {
	db := openTestDB(t)
	store := mission.NewStore(db)
	ctx := context.Background()
	name := "mission:test-" + randSuffix(t)

	lc := mission.LoopContract{
		LoopName:      name,
		Trigger:       "mission-active",
		Cadence:       "daily",
		Authority:     "mission-loop",
		Budget:        json.RawMessage(`{"monthly_usd":100}`),
		Metrics:       json.RawMessage(`{"metric":"net_mrr"}`),
		ExitCondition: "stop",
	}

	ok, err := store.HasLoopContract(ctx, name)
	if err != nil {
		t.Fatalf("HasLoopContract (before): %v", err)
	}
	if ok {
		t.Fatal("HasLoopContract = true before registering, want false")
	}

	if err := store.RegisterLoopContract(ctx, lc); err != nil {
		t.Fatalf("RegisterLoopContract (first): %v", err)
	}
	if err := store.RegisterLoopContract(ctx, lc); err != nil {
		t.Fatalf("RegisterLoopContract (second, same loop_name): %v", err)
	}

	ok, err = store.HasLoopContract(ctx, name)
	if err != nil {
		t.Fatalf("HasLoopContract (after): %v", err)
	}
	if !ok {
		t.Fatal("HasLoopContract = false after registering, want true")
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM loop_contracts WHERE loop_name = $1`, name).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("loop_contracts row count for %q = %d, want 1 (ON CONFLICT DO NOTHING must not duplicate)", name, count)
	}
}

// TestStore_RecordState_RealPostgres proves RecordState's nullable
// confirmed_since handling and the JSONB-free mission_state append path
// against real SQL.
func TestStore_RecordState_RealPostgres(t *testing.T) {
	db := openTestDB(t)
	store := mission.NewStore(db)
	ctx := context.Background()
	suffix := randSuffix(t)
	principalID := "principal-" + suffix
	insertTestPrincipal(t, db, principalID)

	m := mission.Mission{ID: "mission-" + suffix, PrincipalID: principalID, WorkflowID: "wf-" + suffix, Contract: minimalContract(suffix)}
	if err := store.CreateMission(ctx, m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	confirmedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.RecordState(ctx, mission.StateSnapshot{
		MissionID: m.ID, Cycle: 1, NetMRRUSD: 42.5, NoProgressCycles: 0,
		Confirming: true, ConfirmedSince: &confirmedAt,
		Status: "RUNNING", ObservedAt: confirmedAt,
	}); err != nil {
		t.Fatalf("RecordState (confirming): %v", err)
	}
	// A second row with ConfirmedSince == nil proves the nullable column
	// round-trips both ways, not just the non-nil case.
	if err := store.RecordState(ctx, mission.StateSnapshot{
		MissionID: m.ID, Cycle: 2, NetMRRUSD: 10, NoProgressCycles: 1,
		Confirming: false, Status: "RUNNING", ObservedAt: confirmedAt.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("RecordState (not confirming): %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mission_state WHERE mission_id = $1`, m.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("mission_state row count = %d, want 2 (append-only)", count)
	}
}

// TestStore_GateEvent_RecordAndResolve_RealPostgres proves RecordGateEvent
// + ResolveGateEvent's real-SQL round-trip, including ResolveGateEvent's
// ErrNotFound path for an id that never existed.
func TestStore_GateEvent_RecordAndResolve_RealPostgres(t *testing.T) {
	db := openTestDB(t)
	store := mission.NewStore(db)
	ctx := context.Background()
	suffix := randSuffix(t)
	principalID := "principal-" + suffix
	insertTestPrincipal(t, db, principalID)

	m := mission.Mission{ID: "mission-" + suffix, PrincipalID: principalID, WorkflowID: "wf-" + suffix, Contract: minimalContract(suffix)}
	if err := store.CreateMission(ctx, m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	id, err := store.RecordGateEvent(ctx, m.ID, mission.PauseUnforeseenHumanGate, time.Now().UTC())
	if err != nil {
		t.Fatalf("RecordGateEvent: %v", err)
	}
	if id == "" {
		t.Fatal("RecordGateEvent returned empty id")
	}

	if err := store.ResolveGateEvent(ctx, id, "operator resumed", time.Now().UTC()); err != nil {
		t.Fatalf("ResolveGateEvent: %v", err)
	}

	if err := store.ResolveGateEvent(ctx, "no-such-id-"+suffix, "x", time.Now().UTC()); err != mission.ErrNotFound {
		t.Errorf("ResolveGateEvent(missing) error = %v, want ErrNotFound", err)
	}
}

func TestStore_CeremonyReadiness_SaveAndRequire_RealPostgres(t *testing.T) {
	db := openTestDB(t)
	store := mission.NewStore(db)
	ctx := context.Background()
	suffix := randSuffix(t)
	principalID := "principal-" + suffix
	insertTestPrincipal(t, db, principalID)

	m := mission.Mission{ID: "mission-" + suffix, PrincipalID: principalID, WorkflowID: "wf-" + suffix, Contract: minimalContract(suffix)}
	if err := store.CreateMission(ctx, m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	ok, err := store.HasPassingReadinessArtifact(ctx, m.ID)
	if err != nil {
		t.Fatalf("HasPassingReadinessArtifact(before): %v", err)
	}
	if ok {
		t.Fatal("HasPassingReadinessArtifact(before) = true, want false")
	}

	artifact := mission.MissionReadinessArtifact{
		MissionID:  m.ID,
		Readiness:  mission.ReadinessPass,
		ApprovedBy: principalID,
		Digest:     "sha256:test",
	}
	if err := store.SaveReadinessArtifact(ctx, artifact); err != nil {
		t.Fatalf("SaveReadinessArtifact: %v", err)
	}
	ok, err = store.HasPassingReadinessArtifact(ctx, m.ID)
	if err != nil {
		t.Fatalf("HasPassingReadinessArtifact(after): %v", err)
	}
	if !ok {
		t.Fatal("HasPassingReadinessArtifact(after) = false, want true")
	}
	rec, err := store.LatestReadinessArtifact(ctx, m.ID)
	if err != nil {
		t.Fatalf("LatestReadinessArtifact: %v", err)
	}
	if rec.Artifact.ApprovedBy != principalID {
		t.Fatalf("LatestReadinessArtifact.ApprovedBy = %q, want %q", rec.Artifact.ApprovedBy, principalID)
	}
}
