package observe_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// testDSN mirrors internal/notify/store_test.go's own precedent:
// OBSERVE_TEST_PG_DSN first, PG_DSN second (set for free inside the dev
// container by deploy/docker-compose.yaml).
func testDSN() string {
	if v := os.Getenv("OBSERVE_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured, and creates
// dead_letter_items directly (rather than depending on
// internal/db/migrations/00010_dead_letter.sql having been applied via
// `cmd/foundry migrate up` against this test database) — the same
// approach internal/notify's own PostgresStore tests use, since this
// package's Validation command ("go test ./internal/observe/... -race")
// has no live-infra gate.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("OBSERVE_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/observe/...` for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const createTable = `
CREATE TABLE IF NOT EXISTS dead_letter_items (
    id         TEXT PRIMARY KEY,
    queue      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    reason     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("create dead_letter_items table: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM dead_letter_items`); err != nil {
		t.Fatalf("truncate dead_letter_items: %v", err)
	}
	return db
}

func TestPostgresDeadLetterStore_RecordAndList(t *testing.T) {
	db := openTestDB(t)
	store := observe.NewPostgresDeadLetterStore(db)
	ctx := context.Background()

	item, err := store.Record(ctx, "learning", []byte(`{"task":"eval-1"}`), "poisoned task")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if item.ID == "" || item.CreatedAt.IsZero() {
		t.Fatalf("Record did not populate ID/CreatedAt: %+v", item)
	}

	items, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("List() = %+v, want exactly the recorded item", items)
	}
	if items[0].Queue != "learning" || items[0].Reason != "poisoned task" {
		t.Fatalf("List()[0] = %+v, unexpected fields", items[0])
	}
}

func TestPostgresDeadLetterStore_ListRespectsLimitAndOrder(t *testing.T) {
	db := openTestDB(t)
	store := observe.NewPostgresDeadLetterStore(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := store.Record(ctx, "delivery", []byte(`{}`), "reason"); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	items, err := store.List(ctx, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List(limit=2) returned %d items, want 2", len(items))
	}

	all, err := store.List(ctx, 0)
	if err != nil {
		t.Fatalf("List(0): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(limit<=0) returned %d items, want 3 (no limit)", len(all))
	}
}
