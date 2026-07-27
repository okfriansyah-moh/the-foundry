package notify_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// scriptedSender returns one notify.SendResult per call, in order,
// looping the last result once exhausted — enough control to test
// retry/backoff/dead-letter transitions deterministically.
type scriptedSender struct {
	mu      sync.Mutex
	results []notify.SendResult
	calls   int
}

func (s *scriptedSender) Send(_ context.Context, _, _ string) notify.SendResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	if i >= len(s.results) {
		i = len(s.results) - 1
	}
	s.calls++
	return s.results[i]
}

func (s *scriptedSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newEngine(sender notify.Sender) (*notify.Engine, *notify.MemoryStore) {
	store := notify.NewMemoryStore()
	limiter := notify.NewRateLimiter(notify.Limits{
		GlobalPerSecond: 1000, GlobalBurst: 1000,
		PrivatePerSecond: 1000, PrivateBurst: 1000,
		GroupPerMinute: 60000, GroupBurst: 1000,
	})
	engine := notify.NewEngine(store, sender, limiter, notify.Config{MaxAttempts: 3, BackoffBase: time.Millisecond})
	return engine, store
}

func TestEngine_P0BypassesBatchingAndDeliversImmediately(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{OK: true}}}
	engine, store := newEngine(sender)
	ctx := context.Background()

	ev := notify.Event{Class: notify.P0Critical, ChatID: "chat-1", ChatType: notify.ChatPrivate, Workflow: "wf-1", Text: "critical!", DedupeKey: "p0-1"}
	if err := engine.Ingest(ctx, ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if engine.Batcher().Pending() != 0 {
		t.Fatal("P0 must never enter the batcher")
	}

	sent, dead, err := engine.DeliverPending(ctx, 10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sent != 1 || dead != 0 {
		t.Fatalf("want 1 sent, 0 dead-lettered, got sent=%d dead=%d", sent, dead)
	}
	rows := store.Snapshot()
	if len(rows) != 1 || rows[0].State != notify.StateSent {
		t.Fatalf("want 1 sent row, got %+v", rows)
	}
}

func TestEngine_P3EventsCoalesceUntilTick(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{OK: true}}}
	engine, store := newEngine(sender)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		ev := notify.Event{Class: notify.P3Progress, ChatID: "chat-1", ChatType: notify.ChatPrivate, Workflow: "wf-1", Text: "progress", DedupeKey: "p3"}
		if err := engine.Ingest(ctx, ev); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}
	if len(store.Snapshot()) != 0 {
		t.Fatal("P3 events must not be enqueued before the batch window flushes")
	}

	if err := engine.TickBatches(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	rows := store.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 coalesced digest row, got %d", len(rows))
	}
}

func TestEngine_RetryableFailureRetriesThenDeadLettersAfterMaxAttempts(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{
		{Retryable: true, Err: errBoom},
		{Retryable: true, Err: errBoom},
		{Retryable: true, Err: errBoom},
	}}
	engine, store := newEngine(sender)
	ctx := context.Background()

	ev := notify.Event{Class: notify.P0Critical, ChatID: "chat-1", ChatType: notify.ChatPrivate, Workflow: "wf-1", Text: "x", DedupeKey: "retry-1"}
	if err := engine.Ingest(ctx, ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, _, err := engine.DeliverPending(ctx, 10); err != nil {
			t.Fatalf("deliver round %d: %v", i, err)
		}
		time.Sleep(5 * time.Millisecond) // clear the (short, test-configured) backoff before the next round
	}

	rows := store.Snapshot()
	if len(rows) != 1 || rows[0].State != notify.StateFailed {
		t.Fatalf("want the row dead-lettered after MaxAttempts=3 retryable failures, got %+v", rows)
	}
	if sender.callCount() != 3 {
		t.Fatalf("want exactly 3 send attempts (MaxAttempts), got %d", sender.callCount())
	}
}

func TestEngine_NonRetryableFailureDeadLettersImmediately(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{Retryable: false, Err: errBoom}}}
	engine, store := newEngine(sender)
	ctx := context.Background()

	ev := notify.Event{Class: notify.P0Critical, ChatID: "chat-1", ChatType: notify.ChatPrivate, Workflow: "wf-1", Text: "x", DedupeKey: "nonretry-1"}
	if err := engine.Ingest(ctx, ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if _, _, err := engine.DeliverPending(ctx, 10); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	rows := store.Snapshot()
	if len(rows) != 1 || rows[0].State != notify.StateFailed {
		t.Fatalf("non-retryable failure must dead-letter on the first attempt, got %+v", rows)
	}
	if sender.callCount() != 1 {
		t.Fatalf("want exactly 1 send attempt for a non-retryable failure, got %d", sender.callCount())
	}
}

func TestEngine_RateLimitedRowStaysPendingNotDropped(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{OK: true}}}
	store := notify.NewMemoryStore()
	limiter := notify.NewRateLimiter(notify.Limits{
		GlobalPerSecond: 0, GlobalBurst: 0, // no tokens at all
		PrivatePerSecond: 1000, PrivateBurst: 1000,
		GroupPerMinute: 60000, GroupBurst: 1000,
	})
	engine := notify.NewEngine(store, sender, limiter, notify.Config{})
	ctx := context.Background()

	ev := notify.Event{Class: notify.P0Critical, ChatID: "chat-1", ChatType: notify.ChatPrivate, Workflow: "wf-1", Text: "x", DedupeKey: "rl-1"}
	if err := engine.Ingest(ctx, ev); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	sent, dead, err := engine.DeliverPending(ctx, 10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if sent != 0 || dead != 0 {
		t.Fatalf("rate-limited row must be neither sent nor dead-lettered, got sent=%d dead=%d", sent, dead)
	}
	rows := store.Snapshot()
	if len(rows) != 1 || rows[0].State != notify.StatePending {
		t.Fatalf("rate-limited row must remain pending (never dropped), got %+v", rows)
	}
}

// TestEngine_DeliverPending_UpdatesQueueDepthMetric proves DeliverPending
// wires internal/observe's queue_depth gauge (docs/PLAN.md Task 31) from a
// real CountPending read, net of what this call just claimed — not from
// the pre-claim snapshot.
func TestEngine_DeliverPending_UpdatesQueueDepthMetric(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{OK: true}}}
	engine, store := newEngine(sender)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ev := notify.Event{
			Class: notify.P0Critical, ChatID: "chat-1", ChatType: notify.ChatPrivate,
			Workflow: "wf-1", Text: "x", DedupeKey: "queue-depth-" + string(rune('a'+i)),
		}
		if err := engine.Ingest(ctx, ev); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}

	before, err := store.CountPending(ctx)
	if err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if before != 3 {
		t.Fatalf("count pending before delivery = %d, want 3", before)
	}

	// Claim only 1 of 3 so a real, non-zero depth remains to observe.
	if _, _, err := engine.DeliverPending(ctx, 1); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	remaining, err := store.CountPending(ctx)
	if err != nil {
		t.Fatalf("count pending after delivery: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("count pending after delivery = %d, want 2", remaining)
	}
	if got := testutil.ToFloat64(observe.QueueDepth.WithLabelValues("notifications")); got != float64(remaining) {
		t.Fatalf("foundry_queue_depth{queue=notifications} = %v, want %v (matching store.CountPending)", got, remaining)
	}
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }
