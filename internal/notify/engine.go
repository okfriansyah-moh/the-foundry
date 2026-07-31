package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// scheduleRetryTimeout bounds the persistence write in scheduleBackoff so a
// slow or unavailable store cannot hang the delivery loop (the in-memory
// schedule still paces this process regardless).
const scheduleRetryTimeout = 5 * time.Second

// Config sizes Engine's batching and retry/backoff behavior.
type Config struct {
	// MaxAttempts is how many send attempts a notification gets before
	// it is dead-lettered. Defaults to 5.
	MaxAttempts int
	// BackoffBase/BackoffMax size the fallback exponential backoff used
	// when Telegram does not supply an authoritative retry_after
	// (docs/foundry/docs/operations/telegram.md §19.17's fallback:
	// base 2s, multiplier 2, cap 15m).
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// BatchWindow/MaxBatchSize size the Batcher for P2/P3 coalescing.
	BatchWindow  time.Duration
	MaxBatchSize int
	Logger       *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = 2 * time.Second
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 15 * time.Minute
	}
	if c.BatchWindow <= 0 {
		c.BatchWindow = 3 * time.Second
	}
	if c.MaxBatchSize <= 0 {
		c.MaxBatchSize = 20
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Engine is the Telegram notification engine: classification, flood
// control, batching, and outbound delivery with retry/backoff and a
// dead-letter path (docs/PLAN.md Task 30).
type Engine struct {
	store   Store
	sender  Sender
	limiter *RateLimiter
	batcher *Batcher
	cfg     Config

	mu          sync.Mutex
	nextAttempt map[string]time.Time // notification id -> not-before
}

// NewEngine wires store/sender/limiter together with cfg (zero-value
// Config gets sane defaults).
func NewEngine(store Store, sender Sender, limiter *RateLimiter, cfg Config) *Engine {
	cfg = cfg.withDefaults()
	return &Engine{
		store:       store,
		sender:      sender,
		limiter:     limiter,
		batcher:     NewBatcher(cfg.BatchWindow, cfg.MaxBatchSize),
		cfg:         cfg,
		nextAttempt: make(map[string]time.Time),
	}
}

// Batcher exposes the underlying Batcher — callers (e.g. the soak test)
// use Batcher().Pending() to confirm batching actually engaged.
func (e *Engine) Batcher() *Batcher { return e.batcher }

// enqueuePayload is the notifications.payload JSON shape this engine
// writes and reads back in DeliverPending.
type enqueuePayload struct {
	Text     string   `json:"text"`
	ChatType ChatType `json:"chat_type"`
}

// Ingest admits ev into the engine. P0/P1 events (Class.Immediate) are
// enqueued for delivery immediately; P2/P3 events are handed to the
// Batcher and only enqueued once their coalescing window flushes
// (immediately, if this call reached the batch size cap, or later via
// TickBatches).
func (e *Engine) Ingest(ctx context.Context, ev Event) error {
	if ev.Class.Immediate() {
		return e.enqueue(ctx, ev.DedupeKey, ev.ChatID, ev.ChatType, ev.Class.String(), ev.Text)
	}
	if digest, ready := e.batcher.Add(ev, time.Now()); ready {
		return e.enqueue(ctx, digest.DedupeKey, digest.ChatID, digest.ChatType, "digest", digest.Text)
	}
	return nil
}

// TickBatches flushes every P2/P3 aggregation key whose coalescing
// window has elapsed as of now, enqueueing one digest per flushed key.
func (e *Engine) TickBatches(ctx context.Context, now time.Time) error {
	for _, digest := range e.batcher.Tick(now) {
		if err := e.enqueue(ctx, digest.DedupeKey, digest.ChatID, digest.ChatType, "digest", digest.Text); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) enqueue(ctx context.Context, id, chatID string, chatType ChatType, class, text string) error {
	payload, err := json.Marshal(enqueuePayload{Text: text, ChatType: chatType})
	if err != nil {
		return fmt.Errorf("notify: marshal payload for %s: %w", id, err)
	}
	if err := e.store.Enqueue(ctx, id, "telegram", chatID, class, payload); err != nil {
		return fmt.Errorf("notify: enqueue %s: %w", id, err)
	}
	return nil
}

// DeliverPending claims up to limit pending notifications and attempts
// delivery for each whose in-memory backoff has elapsed and whose
// RateLimiter bucket currently has a token. Rows that are rate-limited
// or still backing off this round are left pending for a later call —
// they are never dropped. Returns how many sent and how many were
// dead-lettered this call.
func (e *Engine) DeliverPending(ctx context.Context, limit int) (sent, deadLettered int, err error) {
	rows, err := e.store.ClaimPending(ctx, limit)
	if err != nil {
		return 0, 0, err
	}

	// queue_depth (docs/PLAN.md Task 31): read via defer so it reflects
	// depth net of whatever this call actually drains (rows marked sent/
	// dead-lettered above), no matter which return statement below exits
	// — not the stale pre-processing snapshot. A metrics-read failure must
	// never block real delivery, so it is logged, not propagated.
	defer func() {
		if depth, countErr := e.store.CountPending(ctx); countErr != nil {
			e.cfg.Logger.Error("notify: count pending for queue_depth metric", "error", countErr)
		} else {
			observe.SetQueueDepth("notifications", depth)
		}
	}()

	now := time.Now()
	for _, row := range rows {
		e.mu.Lock()
		notBefore, scheduled := e.nextAttempt[row.ID]
		e.mu.Unlock()
		if scheduled && now.Before(notBefore) {
			continue
		}

		var decoded enqueuePayload
		if unmarshalErr := json.Unmarshal(row.Payload, &decoded); unmarshalErr != nil {
			// A malformed payload can never be retried into success —
			// dead-letter immediately rather than spin forever on it.
			if err := e.store.MarkDeadLetter(ctx, row.ID, fmt.Sprintf("malformed payload: %v", unmarshalErr)); err != nil {
				return sent, deadLettered, err
			}
			deadLettered++
			continue
		}
		if decoded.ChatType == "" {
			decoded.ChatType = ChatPrivate
		}

		if !e.limiter.Allow(row.Target, decoded.ChatType) {
			continue
		}

		result := e.sender.Send(ctx, row.Target, decoded.Text)
		if result.OK {
			if err := e.store.MarkSent(ctx, row.ID); err != nil {
				return sent, deadLettered, err
			}
			e.clearBackoff(row.ID)
			sent++
			continue
		}

		attemptsSoFar := row.Attempts + 1
		if !result.Retryable || attemptsSoFar >= e.cfg.MaxAttempts {
			if err := e.store.MarkDeadLetter(ctx, row.ID, errString(result.Err)); err != nil {
				return sent, deadLettered, err
			}
			e.clearBackoff(row.ID)
			deadLettered++
			continue
		}

		if err := e.store.MarkAttemptFailed(ctx, row.ID, errString(result.Err)); err != nil {
			return sent, deadLettered, err
		}
		e.scheduleBackoff(row.ID, attemptsSoFar, result.RetryAfter)
	}
	return sent, deadLettered, nil
}

// scheduleBackoff sets id's not-before time. Telegram's retry_after
// (retryAfter > 0) is authoritative per §19.17; otherwise this falls
// back to exponential backoff from cfg.BackoffBase, capped at
// cfg.BackoffMax.
func (e *Engine) scheduleBackoff(id string, attempt int, retryAfter time.Duration) {
	delay := retryAfter
	if delay <= 0 {
		delay = e.cfg.BackoffBase
		for i := 1; i < attempt; i++ {
			delay *= 2
			if delay > e.cfg.BackoffMax {
				delay = e.cfg.BackoffMax
				break
			}
		}
	}
	// Compute the not-before time once so the in-memory schedule and the
	// persisted schedule below are byte-for-byte the same instant (no drift
	// from a second time.Now() call).
	notBefore := time.Now().Add(delay)
	e.mu.Lock()
	e.nextAttempt[id] = notBefore
	e.mu.Unlock()
	// Persist the not-before time so the backoff (and Telegram's
	// authoritative retry_after) survives a daemon restart (Task 112). A
	// persistence failure must not block delivery — the in-memory schedule
	// above still paces this process; log and continue. The write is bounded
	// so a slow or unavailable store cannot hang the delivery loop.
	ctx, cancel := context.WithTimeout(context.Background(), scheduleRetryTimeout)
	defer cancel()
	if err := e.store.ScheduleRetry(ctx, id, notBefore); err != nil {
		e.cfg.Logger.Error("notify: persist retry schedule", "id", id, "error", err)
	}
}

func (e *Engine) clearBackoff(id string) {
	e.mu.Lock()
	delete(e.nextAttempt, id)
	e.mu.Unlock()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Run drives the engine's batch-flush and delivery loop until ctx is
// done, ticking every interval. Production callers (e.g. cmd/foundryd)
// use this; tests call TickBatches/DeliverPending directly for
// deterministic control.
func (e *Engine) Run(ctx context.Context, interval time.Duration, claimLimit int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := e.TickBatches(ctx, now); err != nil {
				e.cfg.Logger.Error("notify: tick batches", "error", err)
			}
			if _, _, err := e.DeliverPending(ctx, claimLimit); err != nil {
				e.cfg.Logger.Error("notify: deliver pending", "error", err)
			}
		}
	}
}
