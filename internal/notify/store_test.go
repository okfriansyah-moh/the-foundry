package notify_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

// testDSN mirrors internal/ledger/extops's testDSN precedent:
// NOTIFY_TEST_PG_DSN first, PG_DSN second (set for free inside the dev
// container by deploy/docker-compose.yaml).
func testDSN() string {
	if v := os.Getenv("NOTIFY_TEST_PG_DSN"); v != "" {
		return v
	}
	return os.Getenv("PG_DSN")
}

// openTestDB skips the calling test if no DSN is configured — this is
// why `go test ./internal/notify/... -race` passes with no live infra;
// `make test` (Docker, PG_DSN set) exercises these for real.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("NOTIFY_TEST_PG_DSN/PG_DSN not set — skipping; run via `make test` or `docker compose run --rm dev go test ./internal/notify/...` for a real Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const createTable = `
CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    channel    TEXT NOT NULL,
    target     TEXT NOT NULL,
    class      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('pending', 'sent', 'failed', 'acked')) DEFAULT 'pending',
    attempts   INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at    TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ
)`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("create notifications table: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM notifications`); err != nil {
		t.Fatalf("truncate notifications: %v", err)
	}
	return db
}

func TestPostgresStore_EnqueueIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	store := notify.NewPostgresStore(db)
	ctx := context.Background()

	if err := store.Enqueue(ctx, "n1", "telegram", "chat-1", "message", []byte(`{"text":"a"}`)); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := store.Enqueue(ctx, "n1", "telegram", "chat-1", "message", []byte(`{"text":"b"}`)); err != nil {
		t.Fatalf("second enqueue (should be no-op): %v", err)
	}

	rows, err := store.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row after duplicate enqueue, got %d", len(rows))
	}
	// Compare decoded values, not raw bytes: Postgres's JSONB column
	// re-serializes on read (e.g. adds a space after ':'), which is a
	// harmless storage-format detail, not a sign the second Enqueue
	// overwrote the first row.
	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rows[0].Payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.Text != "a" {
		t.Fatalf("second enqueue must not overwrite the first row, got payload %s", rows[0].Payload)
	}
}

func TestPostgresStore_DeliveryLifecycle(t *testing.T) {
	db := openTestDB(t)
	store := notify.NewPostgresStore(db)
	ctx := context.Background()

	if err := store.Enqueue(ctx, "n2", "telegram", "chat-1", "message", []byte(`{"text":"hi"}`)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if err := store.MarkAttemptFailed(ctx, "n2", "boom"); err != nil {
		t.Fatalf("mark attempt failed: %v", err)
	}
	rows, err := store.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	if len(rows) != 1 || rows[0].Attempts != 1 || rows[0].State != notify.StatePending {
		t.Fatalf("want 1 pending row with attempts=1, got %+v", rows)
	}

	if err := store.MarkDeadLetter(ctx, "n2", "gave up"); err != nil {
		t.Fatalf("mark dead letter: %v", err)
	}
	rows, err = store.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("claim pending after dead letter: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("dead-lettered row must not be claimable as pending, got %+v", rows)
	}

	if err := store.Enqueue(ctx, "n3", "telegram", "chat-1", "message", []byte(`{"text":"hi2"}`)); err != nil {
		t.Fatalf("enqueue n3: %v", err)
	}
	if err := store.MarkSent(ctx, "n3"); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	rows, err = store.ClaimPending(ctx, 10)
	if err != nil {
		t.Fatalf("claim pending after sent: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("sent row must not be claimable as pending, got %+v", rows)
	}
}

func TestPostgresStore_MarkSentUnknownID(t *testing.T) {
	db := openTestDB(t)
	store := notify.NewPostgresStore(db)

	if err := store.MarkSent(context.Background(), "does-not-exist"); err != notify.ErrNotificationNotFound {
		t.Fatalf("want ErrNotificationNotFound, got %v", err)
	}
}
