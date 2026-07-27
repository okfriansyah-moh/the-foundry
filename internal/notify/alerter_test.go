package notify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

func TestEngineAlerter_IngestsImmediateP1Event(t *testing.T) {
	engine, store := newEngine(&scriptedSender{results: []notify.SendResult{{OK: true}}})
	alerter := notify.EngineAlerter{Engine: engine, ChatID: "ops-chat", ChatType: notify.ChatPrivate}

	err := alerter.Alert(context.Background(), observe.DeadLetterAlert{
		ItemID: "item-1",
		Queue:  "learning",
		Reason: "poisoned task: schema validation failed",
	})
	if err != nil {
		t.Fatalf("Alert: %v", err)
	}

	rows := store.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("got %d enqueued notifications, want 1", len(rows))
	}
	row := rows[0]
	if row.Target != "ops-chat" {
		t.Errorf("Target = %q, want ops-chat", row.Target)
	}
	if row.Class != notify.P1Command.String() {
		t.Errorf("Class = %q, want %q (P1 — dedicated message, never batched)", row.Class, notify.P1Command.String())
	}
	if row.State != notify.StatePending {
		// Ingest for an Immediate class enqueues directly rather than
		// going through the Batcher — it should be immediately claimable.
		t.Errorf("State = %q, want pending (ready for immediate delivery)", row.State)
	}
	if !strings.Contains(row.ID, "learning") || !strings.Contains(row.ID, "item-1") {
		t.Errorf("dedupe key %q should identify the dead-lettered queue/item", row.ID)
	}
}

func TestEngineAlerter_DeliversThroughRealEngine(t *testing.T) {
	sender := &scriptedSender{results: []notify.SendResult{{OK: true}}}
	engine, store := newEngine(sender)
	alerter := notify.EngineAlerter{Engine: engine, ChatID: "ops-chat", ChatType: notify.ChatPrivate}

	if err := alerter.Alert(context.Background(), observe.DeadLetterAlert{ItemID: "item-2", Queue: "learning", Reason: "poisoned"}); err != nil {
		t.Fatalf("Alert: %v", err)
	}

	sent, deadLettered, err := engine.DeliverPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if sent != 1 || deadLettered != 0 {
		t.Fatalf("DeliverPending = sent=%d deadLettered=%d, want sent=1 deadLettered=0", sent, deadLettered)
	}

	rows := store.Snapshot()
	if len(rows) != 1 || rows[0].State != notify.StateSent {
		t.Fatalf("expected the P1 alert to end up Sent, got %+v", rows)
	}
}
