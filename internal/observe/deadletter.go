package observe

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// DeadLetterItems counts poisoned work items recorded to a DeadLetterStore,
// by queue — docs/PLAN.md Task 33's "dead-letter table" half.
var DeadLetterItems = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "foundry_dead_letter_items_total",
	Help: "Count of work items recorded to the dead-letter store, by queue (docs/PLAN.md Task 33).",
}, []string{"queue"})

func init() {
	Registry.MustRegister(DeadLetterItems)
}

// DeadLetterItem is one recorded poisoned work item.
type DeadLetterItem struct {
	ID        string
	Queue     string
	Payload   []byte
	Reason    string
	CreatedAt time.Time
}

// DeadLetterStore persists poisoned work items that a queue/lane gave up
// on. This is a distinct, general-purpose table from
// internal/notify.Store's own dead-letter path (that one is specific to
// outbound Telegram notifications and reuses the notifications table's
// existing 'failed' state, per Task 30's own decision) — this store is
// for poisoned intake/queue work items generally.
type DeadLetterStore interface {
	// Record durably records a poisoned item and returns it with its
	// generated ID and CreatedAt populated.
	Record(ctx context.Context, queue string, payload []byte, reason string) (DeadLetterItem, error)
	// List returns up to limit recorded items, most recent first (limit <=
	// 0 means no limit) — used by operators/tests to inspect what was
	// dead-lettered.
	List(ctx context.Context, limit int) ([]DeadLetterItem, error)
}

// Alerter sends the P1 dead-letter alert docs/PLAN.md Task 33's Steps
// require ("dead-letter table + P1 alert"). It is deliberately a minimal,
// self-contained interface — not internal/notify.Event or notify.Engine
// directly — because internal/notify already imports internal/observe
// (for the queue_depth metric, Task 31), so observe importing notify back
// would be a compile-time import cycle. internal/notify/alerter.go defines
// the adapter that lets *notify.Engine satisfy this interface instead,
// reusing Task 30's engine rather than duplicating it, per this card's own
// Steps.
type Alerter interface {
	Alert(ctx context.Context, a DeadLetterAlert) error
}

// DeadLetterAlert is what RecordAndAlert hands to an Alerter for one
// poisoned item.
type DeadLetterAlert struct {
	ItemID string
	Queue  string
	Reason string
}

// RecordAndAlert records item to store, increments DeadLetterItems, and —
// if alerter is non-nil — sends the P1 alert. A failing alert send is
// returned as an error (the record itself already succeeded and is not
// rolled back: losing the alert must never look like losing the
// dead-letter evidence).
func RecordAndAlert(ctx context.Context, store DeadLetterStore, alerter Alerter, queue string, payload []byte, reason string) (DeadLetterItem, error) {
	item, err := store.Record(ctx, queue, payload, reason)
	if err != nil {
		return DeadLetterItem{}, fmt.Errorf("observe: record dead-letter item: %w", err)
	}
	DeadLetterItems.WithLabelValues(queue).Inc()

	if alerter == nil {
		return item, nil
	}
	if err := alerter.Alert(ctx, DeadLetterAlert{ItemID: item.ID, Queue: queue, Reason: reason}); err != nil {
		return item, fmt.Errorf("observe: alert for dead-lettered item %s: %w", item.ID, err)
	}
	return item, nil
}

// newDeadLetterID returns a random 128-bit id, hex-encoded — the same
// scheme internal/ledger/extops.newOpID and internal/kernel/lease.go's
// fencing tokens already use.
func newDeadLetterID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("observe: generate dead-letter id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// MemoryDeadLetterStore is an in-memory DeadLetterStore: this package's
// own unit tests and the drill script (test/drill/brownout) both need no
// live-infra dependency, matching this task's Validation command ("make
// drill-brownout" + unit tests, not a Docker/Postgres-gated command).
// PostgresDeadLetterStore is the durable production implementation.
type MemoryDeadLetterStore struct {
	mu    sync.Mutex
	items []DeadLetterItem
}

// NewMemoryDeadLetterStore constructs an empty MemoryDeadLetterStore.
func NewMemoryDeadLetterStore() *MemoryDeadLetterStore {
	return &MemoryDeadLetterStore{}
}

func (m *MemoryDeadLetterStore) Record(_ context.Context, queue string, payload []byte, reason string) (DeadLetterItem, error) {
	id, err := newDeadLetterID()
	if err != nil {
		return DeadLetterItem{}, err
	}
	item := DeadLetterItem{
		ID:        id,
		Queue:     queue,
		Payload:   append([]byte(nil), payload...),
		Reason:    reason,
		CreatedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	m.items = append(m.items, item)
	m.mu.Unlock()
	return item, nil
}

func (m *MemoryDeadLetterStore) List(_ context.Context, limit int) ([]DeadLetterItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DeadLetterItem, len(m.items))
	copy(out, m.items)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// PostgresDeadLetterStore is the Postgres-backed DeadLetterStore, wrapping
// internal/db/migrations/00010_dead_letter.sql's dead_letter_items table.
type PostgresDeadLetterStore struct {
	db *sql.DB
}

// NewPostgresDeadLetterStore wraps an existing *sql.DB (opened with the
// pgx driver, matching internal/ledger/extops.NewStore's precedent).
func NewPostgresDeadLetterStore(db *sql.DB) *PostgresDeadLetterStore {
	return &PostgresDeadLetterStore{db: db}
}

func (s *PostgresDeadLetterStore) Record(ctx context.Context, queue string, payload []byte, reason string) (DeadLetterItem, error) {
	id, err := newDeadLetterID()
	if err != nil {
		return DeadLetterItem{}, err
	}
	const insert = `
INSERT INTO dead_letter_items (id, queue, payload, reason)
VALUES ($1, $2, $3, $4)
RETURNING created_at`
	var createdAt time.Time
	if err := s.db.QueryRowContext(ctx, insert, id, queue, payload, reason).Scan(&createdAt); err != nil {
		return DeadLetterItem{}, fmt.Errorf("observe: insert dead-letter item: %w", err)
	}
	return DeadLetterItem{ID: id, Queue: queue, Payload: payload, Reason: reason, CreatedAt: createdAt}, nil
}

func (s *PostgresDeadLetterStore) List(ctx context.Context, limit int) ([]DeadLetterItem, error) {
	// LIMIT NULL is Postgres's own "no limit" spelling — passing limit<=0
	// straight through as LIMIT 0 would instead mean "zero rows", the
	// opposite of this method's documented "limit <= 0 means no limit"
	// contract, so a non-positive limit is translated to a nil bind param.
	var limitArg any
	if limit > 0 {
		limitArg = limit
	}
	const q = `
SELECT id, queue, payload, reason, created_at
FROM dead_letter_items
ORDER BY created_at DESC
LIMIT $1`
	rows, err := s.db.QueryContext(ctx, q, limitArg)
	if err != nil {
		return nil, fmt.Errorf("observe: list dead-letter items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DeadLetterItem
	for rows.Next() {
		var item DeadLetterItem
		if err := rows.Scan(&item.ID, &item.Queue, &item.Payload, &item.Reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("observe: scan dead-letter item: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("observe: list dead-letter items: %w", err)
	}
	return out, nil
}
