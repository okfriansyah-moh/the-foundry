package notify

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore is an in-memory Store implementation: the notifications
// table's shape and state machine, without Postgres. Used by this
// package's unit tests and by test/soak/telegram, both of which must
// run with no live-infra dependency (this task's Validation command is
// `go test ./internal/notify/... -race`, not a Docker/Postgres-gated
// command). PostgresStore is the durable production implementation of
// the same Store interface.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[string]*Notification
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: make(map[string]*Notification)}
}

func (m *MemoryStore) Enqueue(_ context.Context, id, channel, target, class string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.rows[id]; exists {
		return nil // ON CONFLICT DO NOTHING equivalent
	}
	now := time.Now()
	m.rows[id] = &Notification{
		ID:        id,
		Channel:   channel,
		Target:    target,
		Class:     class,
		Payload:   append([]byte(nil), payload...),
		State:     StatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return nil
}

func (m *MemoryStore) ClaimPending(_ context.Context, limit int) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []Notification
	for _, row := range m.rows {
		if row.State == StatePending && (row.NextAttemptAt.IsZero() || !row.NextAttemptAt.After(time.Now())) {
			out = append(out, *row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) CountPending(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, row := range m.rows {
		if row.State == StatePending {
			count++
		}
	}
	return count, nil
}

func (m *MemoryStore) MarkSent(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotificationNotFound
	}
	row.State = StateSent
	row.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStore) MarkAttemptFailed(_ context.Context, id, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotificationNotFound
	}
	row.Attempts++
	row.LastError = errMsg
	row.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStore) MarkDeadLetter(_ context.Context, id, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotificationNotFound
	}
	row.State = StateFailed
	row.LastError = "dead-letter: " + errMsg
	row.UpdatedAt = time.Now()
	return nil
}

// ScheduleRetry persists id's not-before time in memory (Task 112 parity).
func (m *MemoryStore) ScheduleRetry(_ context.Context, id string, notBefore time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return ErrNotificationNotFound
	}
	row.NextAttemptAt = notBefore
	row.UpdatedAt = time.Now()
	return nil
}

// MemoryOffsetStore is an in-memory OffsetStore for tests.
type MemoryOffsetStore struct {
	mu      sync.Mutex
	offsets map[string]int64
}

// NewMemoryOffsetStore constructs an empty MemoryOffsetStore.
func NewMemoryOffsetStore() *MemoryOffsetStore {
	return &MemoryOffsetStore{offsets: make(map[string]int64)}
}

// GetOffset implements OffsetStore.
func (m *MemoryOffsetStore) GetOffset(_ context.Context, botID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offsets[botID], nil
}

// SetOffset implements OffsetStore (monotonic).
func (m *MemoryOffsetStore) SetOffset(_ context.Context, botID string, updateID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if updateID > m.offsets[botID] {
		m.offsets[botID] = updateID
	}
	return nil
}

// Snapshot returns a copy of every row, for test assertions.
func (m *MemoryStore) Snapshot() []Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Notification, 0, len(m.rows))
	for _, row := range m.rows {
		out = append(out, *row)
	}
	return out
}
