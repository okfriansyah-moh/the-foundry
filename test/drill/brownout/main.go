// Command brownout is docs/PLAN.md Task 33's drill script: it proves, in
// one deterministic run with no live Temporal/Postgres/network
// dependency, both halves of the card's Acceptance criteria:
//
//   - brownout mode sheds the learning lane first while recovery,
//     delivery, and notification keep draining their own backlogs to
//     completion ("drill shows learning lane paused while delivery
//     completes"), and that pausing is temporary — learning resumes and
//     drains once brownout is disabled again, rather than silently
//     losing its backlog;
//   - a poisoned work item recorded to the dead-letter store fires a
//     real P1 alert through Task 30's notify engine, and that alert is
//     actually deliverable ("DLQ alert fires on a poisoned item").
//
// Each lane's backlog is drained in fixed, deterministic rounds (one
// admitted item per lane per round) rather than real goroutines/sleeps,
// so this drill's outcome never depends on scheduler timing — per
// .ai/skills/qa-testing/SKILL.md's rule against timing-dependent
// synchronization.
//
// Run: `go run ./test/drill/brownout` (wired as `make drill-brownout`).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// itemsPerLane is each lane's simulated starting backlog.
const itemsPerLane = 20

// maxRoundsPerPhase bounds how many rounds a phase (brownout-enabled or
// brownout-disabled) runs — enough for every non-sheddable lane's backlog
// to fully drain (1 item/round), with margin.
const maxRoundsPerPhase = itemsPerLane * 2

func main() {
	if err := run(); err != nil {
		log.Fatalf("drill-brownout FAILED: %v", err)
	}
	fmt.Println("drill-brownout PASSED")
}

func run() error {
	if err := runBrownoutShedOrder(); err != nil {
		return fmt.Errorf("brownout shed-order phase: %w", err)
	}
	if err := runDeadLetterAlert(); err != nil {
		return fmt.Errorf("dead-letter alert phase: %w", err)
	}
	return nil
}

// backlog is one lane's remaining simulated work-item count.
type backlog struct {
	lane      observe.Lane
	remaining int
}

func runBrownoutShedOrder() error {
	cfg, err := observe.LoadQueueConfig("config/queue-priority.yaml")
	if err != nil {
		return fmt.Errorf("load queue config: %w", err)
	}
	brownout := observe.NewBrownoutController(cfg)

	backlogs := map[observe.Lane]*backlog{
		observe.LaneRecovery:     {lane: observe.LaneRecovery, remaining: itemsPerLane},
		observe.LaneDelivery:     {lane: observe.LaneDelivery, remaining: itemsPerLane},
		observe.LaneNotification: {lane: observe.LaneNotification, remaining: itemsPerLane},
		observe.LaneLearning:     {lane: observe.LaneLearning, remaining: itemsPerLane},
	}

	// Phase 1: brownout enabled from the start — recovery/delivery/
	// notification must fully drain; learning must not process a single
	// item ("paused", not merely slowed).
	brownout.SetEnabled(true)
	drainRounds(backlogs, brownout, maxRoundsPerPhase)

	for _, lane := range []observe.Lane{observe.LaneRecovery, observe.LaneDelivery, observe.LaneNotification} {
		if backlogs[lane].remaining != 0 {
			return fmt.Errorf("lane %s: %d items still remaining after brownout phase, want 0 (delivery must complete)", lane, backlogs[lane].remaining)
		}
	}
	if backlogs[observe.LaneLearning].remaining != itemsPerLane {
		return fmt.Errorf("lane learning: %d/%d items processed during brownout, want 0 processed (paused)", itemsPerLane-backlogs[observe.LaneLearning].remaining, itemsPerLane)
	}
	fmt.Printf("phase 1 (brownout ON): recovery/delivery/notification drained to 0; learning held at %d/%d (paused)\n",
		backlogs[observe.LaneLearning].remaining, itemsPerLane)

	// Phase 2: brownout disabled — learning's backlog must resume and
	// fully drain, proving the shed was a pause, not a silent drop.
	brownout.SetEnabled(false)
	drainRounds(backlogs, brownout, maxRoundsPerPhase)

	if backlogs[observe.LaneLearning].remaining != 0 {
		return fmt.Errorf("lane learning: %d items still remaining after brownout was disabled, want 0 (resumed)", backlogs[observe.LaneLearning].remaining)
	}
	fmt.Println("phase 2 (brownout OFF): learning drained to 0 (resumed, not dropped)")
	return nil
}

// drainRounds runs up to rounds iterations; each round processes exactly
// one item from every lane the controller currently admits and that
// still has a remaining backlog.
func drainRounds(backlogs map[observe.Lane]*backlog, brownout *observe.BrownoutController, rounds int) {
	for i := 0; i < rounds; i++ {
		for lane, b := range backlogs {
			if b.remaining == 0 {
				continue
			}
			if !brownout.Admit(lane) {
				continue
			}
			b.remaining--
		}
	}
}

// fakeSender always accepts — this phase proves the alert reaches a real
// notify.Engine and can be delivered, not the Telegram Bot API's own
// transport (that is test/fakes/telegram + Task 30's own soak test's job).
type fakeSender struct{}

func (fakeSender) Send(_ context.Context, _, _ string) notify.SendResult {
	return notify.SendResult{OK: true}
}

func runDeadLetterAlert() error {
	ctx := context.Background()

	store := observe.NewMemoryDeadLetterStore()
	notifyStore := notify.NewMemoryStore()
	limiter := notify.NewRateLimiter(notify.DefaultLimits())
	engine := notify.NewEngine(notifyStore, fakeSender{}, limiter, notify.Config{})
	alerter := notify.EngineAlerter{Engine: engine, ChatID: "ops-alerts", ChatType: notify.ChatPrivate}

	const poisonedQueue = "learning"
	const poisonedReason = "poisoned task: schema validation failed 3x, no-progress detector tripped"

	item, err := observe.RecordAndAlert(ctx, store, alerter, poisonedQueue, []byte(`{"task_id":"eval-42"}`), poisonedReason)
	if err != nil {
		return fmt.Errorf("record poisoned item and send P1 alert: %w", err)
	}

	items, err := store.List(ctx, 10)
	if err != nil {
		return fmt.Errorf("list dead-letter items: %w", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		return fmt.Errorf("dead-letter store has %d items, want exactly the 1 just recorded", len(items))
	}

	sent, deadLettered, err := engine.DeliverPending(ctx, 10)
	if err != nil {
		return fmt.Errorf("deliver pending alerts: %w", err)
	}
	if sent != 1 || deadLettered != 0 {
		return fmt.Errorf("P1 alert delivery: sent=%d deadLettered=%d, want sent=1 deadLettered=0", sent, deadLettered)
	}

	fmt.Printf("dead-letter alert: item=%s queue=%s reason=%q recorded and delivered as a real P1 notification\n", item.ID, item.Queue, item.Reason)
	return nil
}
