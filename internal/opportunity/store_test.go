package opportunity_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
)

// testDSN mirrors internal/mission/store_pg_test.go: OPPORTUNITY_TEST_PG_DSN
// first, PG_DSN second (set for free inside the dev container).
func testDSN() string {
	if v := os.Getenv("OPPORTUNITY_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured, otherwise returns
// a live connection with the Task 100 tables guaranteed to exist (created here
// so the test does not depend on `make migrate-up` having run first).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("set PG_DSN or OPPORTUNITY_TEST_PG_DSN to run opportunity store tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS opportunities (
			id TEXT PRIMARY KEY, statement TEXT NOT NULL,
			submitted_by TEXT NOT NULL DEFAULT '', submitted_at TIMESTAMPTZ,
			source TEXT NOT NULL DEFAULT '', icp JSONB NOT NULL DEFAULT '{}'::jsonb,
			estimated_validation_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
			mvp_budget_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
			max_active_builds INTEGER NOT NULL DEFAULT 1,
			real_validation_signal BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS opportunity_evidence (
			id TEXT PRIMARY KEY, opportunity_id TEXT NOT NULL REFERENCES opportunities(id),
			seq BIGSERIAL, kind TEXT NOT NULL, text TEXT NOT NULL, label TEXT NOT NULL,
			basis TEXT NOT NULL DEFAULT '', source_ref TEXT NOT NULL DEFAULT '',
			observed_at TIMESTAMPTZ, untrusted BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS opportunity_scores (
			id TEXT PRIMARY KEY, opportunity_id TEXT NOT NULL REFERENCES opportunities(id),
			scorecard JSONB NOT NULL, scorecard_digest TEXT NOT NULL,
			config_version TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS opportunity_verdicts (
			id TEXT PRIMARY KEY, opportunity_id TEXT NOT NULL REFERENCES opportunities(id),
			verdict TEXT NOT NULL, unmet_thresholds JSONB NOT NULL DEFAULT '[]'::jsonb,
			scorecard_digest TEXT NOT NULL, thresholds_digest TEXT NOT NULL,
			config_version TEXT NOT NULL, scorecard JSONB NOT NULL, thresholds JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestStoreRoundTripAndReDerive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	store := opportunity.NewStore(db)
	cfg, err := opportunity.LoadConfig("../../config/opportunity-thresholds.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	o := opportunity.Opportunity{
		Idea: opportunity.Idea{Statement: "test idea", Source: "test"},
		ICP: opportunity.ICP{
			Segment:           "s",
			ReachableChannels: []opportunity.Channel{{Name: "c", Reachable: true}},
		},
		EstimatedValidationCostUSD: 50,
		MVPBudgetUSD:               120,
		MaxActiveBuilds:            1,
		RealValidationSignal:       true,
	}
	id, err := store.CreateOpportunity(ctx, o)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	claims := []opportunity.Claim{
		{Kind: opportunity.KindProblem, Text: "p", Label: opportunity.LabelObserved, SourceRef: "s://1#h"},
		{Kind: opportunity.KindWTP, Text: "w", Label: opportunity.LabelObserved, SourceRef: "s://2#h"},
		{Kind: opportunity.KindDistribution, Text: "d", Label: opportunity.LabelObserved, SourceRef: "s://3#h"},
	}
	for _, c := range claims {
		if err := store.AppendEvidence(ctx, id, c); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	loaded, err := store.LoadOpportunity(ctx, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Claims) != len(claims) {
		t.Fatalf("evidence count: got %d want %d", len(loaded.Claims), len(claims))
	}
	if !loaded.RealValidationSignal || loaded.MVPBudgetUSD != 120 {
		t.Fatalf("opportunity fields not round-tripped: %+v", loaded)
	}

	sc := opportunity.Score(loaded, cfg)
	if err := store.RecordScore(ctx, id, sc); err != nil {
		t.Fatalf("record score: %v", err)
	}
	v, unmet := opportunity.Decide(sc, cfg.Thresholds)
	rec, err := store.RecordVerdict(ctx, id, v, unmet, sc, cfg.Thresholds)
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	got, err := store.LatestVerdict(ctx, id)
	if err != nil {
		t.Fatalf("latest verdict: %v", err)
	}
	if got.Verdict != v || got.ScorecardDigest != rec.ScorecardDigest {
		t.Fatalf("verdict mismatch: %+v vs %+v", got, rec)
	}

	// Re-derive from stored evidence and confirm the digest reproduces (this
	// is the property Task 102's kernel gate relies on).
	reScored := opportunity.Score(loaded, cfg)
	reDigest, err := reScored.Digest()
	if err != nil {
		t.Fatalf("re-digest: %v", err)
	}
	if reDigest != got.ScorecardDigest {
		t.Fatalf("scorecard not reproducible from stored evidence: %s != %s", reDigest, got.ScorecardDigest)
	}
}
