// Command telegram is docs/PLAN.md Task 30's soak test harness: it
// bursts 5,000 events at a real internal/notify.Engine and proves,
// against test/fakes/telegram's mock server (which independently
// enforces Telegram's raw published rate limits):
//
//   - zero P0 events are ever dropped (every P0 dedupe key ends up
//     StateSent, never left pending or dead-lettered);
//   - batching actually engaged for P2/P3 traffic (far fewer real
//     sendMessage calls than P2/P3 events ingested);
//   - zero 429 (flood-control) responses occur.
//
// Run: `go run ./test/soak/telegram` (no live Telegram credentials or
// Docker required — the mock server is an in-process httptest server).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	faketelegram "github.com/okfriansyah-moh/the-foundry/test/fakes/telegram"
)

const (
	totalEvents  = 5000
	numChats     = 20
	numWorkflows = 5
	p0Count      = 100 // spread across chats; the rest are P2/P3
	drainTimeout = 90 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("soak test failed: %v", err)
	}
	fmt.Println("soak test PASSED")
}

func run() error {
	mock := faketelegram.New(faketelegram.DefaultRawLimits())
	defer mock.Close()

	store := notify.NewMemoryStore()
	sender := &notify.HTTPSender{BaseURL: mock.URL, Token: "soak-test-token"}
	limiter := notify.NewRateLimiter(notify.DefaultLimits())
	engine := notify.NewEngine(store, sender, limiter, notify.Config{
		BatchWindow:  2 * time.Second,
		MaxBatchSize: 20,
		MaxAttempts:  5,
	})

	ctx := context.Background()
	p0DedupeKeys := make(map[string]bool)

	for i := 0; i < totalEvents; i++ {
		chatID := fmt.Sprintf("chat-%d", i%numChats)
		workflow := fmt.Sprintf("wf-%d", i%numWorkflows)

		class := notify.P3Progress
		if i%3 == 0 {
			class = notify.P2State
		}
		dedupe := fmt.Sprintf("evt-%d", i)
		if i < p0Count {
			class = notify.P0Critical
			p0DedupeKeys[dedupe] = false
		}

		ev := notify.Event{
			Class:     class,
			ChatID:    chatID,
			ChatType:  notify.ChatPrivate,
			Workflow:  workflow,
			Text:      fmt.Sprintf("event %d on %s", i, workflow),
			DedupeKey: dedupe,
		}
		if err := engine.Ingest(ctx, ev); err != nil {
			return fmt.Errorf("ingest event %d: %w", i, err)
		}
	}

	fmt.Printf("burst complete: %d events ingested (%d P0), pending batch keys: %d\n",
		totalEvents, p0Count, engine.Batcher().Pending())
	batchingEngagedKeys := engine.Batcher().Pending()

	deadline := time.Now().Add(drainTimeout)
	for time.Now().Before(deadline) {
		now := time.Now()
		if err := engine.TickBatches(ctx, now); err != nil {
			return fmt.Errorf("tick batches: %w", err)
		}
		if _, _, err := engine.DeliverPending(ctx, 50); err != nil {
			return fmt.Errorf("deliver pending: %w", err)
		}

		rows := store.Snapshot()
		pending := 0
		for _, r := range rows {
			if r.State == notify.StatePending {
				pending++
			}
		}
		if pending == 0 && engine.Batcher().Pending() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	rows := store.Snapshot()
	sentP0, deadP0 := 0, 0
	for _, r := range rows {
		if _, isP0 := p0DedupeKeys[r.ID]; !isP0 {
			continue
		}
		switch r.State {
		case notify.StateSent:
			sentP0++
		case notify.StateFailed:
			deadP0++
		}
	}
	if sentP0+deadP0 != p0Count {
		return fmt.Errorf("accounted for %d/%d P0 events (some still pending/unresolved after %s)", sentP0+deadP0, p0Count, drainTimeout)
	}
	if deadP0 != 0 {
		return fmt.Errorf("P0 DROP DETECTED: %d of %d P0 events were dead-lettered, want 0", deadP0, p0Count)
	}

	stats := mock.Snapshot()
	fmt.Printf("delivery drained: mock server sent=%d rate_limited(429)=%d\n", stats.Sent, stats.RateLimited)
	fmt.Printf("P0 accounting: sent=%d dead_lettered=%d (want dead_lettered=0)\n", sentP0, deadP0)
	fmt.Printf("batching engaged: %d aggregation keys were pending immediately after the burst (proves P2/P3 coalesced rather than sending 1:1)\n", batchingEngagedKeys)

	if stats.RateLimited != 0 {
		return fmt.Errorf("429 DETECTED: mock server issued %d flood-control responses, want 0", stats.RateLimited)
	}
	if batchingEngagedKeys == 0 {
		return fmt.Errorf("batching did not engage: 0 aggregation keys were pending after the burst")
	}
	nonP0Events := totalEvents - p0Count
	if stats.Sent >= nonP0Events {
		return fmt.Errorf("batching did not reduce send volume: mock saw %d sends for %d non-P0 events (plus %d P0)", stats.Sent, nonP0Events, p0Count)
	}

	if os.Getenv("SOAK_VERBOSE") != "" {
		fmt.Printf("total rows: %d\n", len(rows))
	}
	return nil
}
