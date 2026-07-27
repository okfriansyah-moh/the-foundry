package notify_test

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/notify"
)

func p2Event(chatID, workflow, text, dedupe string) notify.Event {
	return notify.Event{Class: notify.P2State, ChatID: chatID, ChatType: notify.ChatPrivate, Workflow: workflow, Text: text, DedupeKey: dedupe}
}

func TestBatcher_CoalescesUntilWindowElapses(t *testing.T) {
	b := notify.NewBatcher(time.Minute, 100)
	now := time.Now()

	if _, ready := b.Add(p2Event("c1", "wf1", "step a done", "e1"), now); ready {
		t.Fatal("first event must not flush before window/max-size")
	}
	if _, ready := b.Add(p2Event("c1", "wf1", "step b done", "e2"), now); ready {
		t.Fatal("second event under the same key must still coalesce, not flush")
	}
	if got := b.Pending(); got != 1 {
		t.Fatalf("want 1 pending aggregation key, got %d", got)
	}

	digests := b.Tick(now.Add(30 * time.Second))
	if len(digests) != 0 {
		t.Fatalf("window has not elapsed yet, want 0 digests, got %d", len(digests))
	}

	digests = b.Tick(now.Add(61 * time.Second))
	if len(digests) != 1 {
		t.Fatalf("want 1 flushed digest after window elapses, got %d", len(digests))
	}
	if digests[0].EventsCount != 2 {
		t.Fatalf("want 2 coalesced events in the digest, got %d", digests[0].EventsCount)
	}
	if b.Pending() != 0 {
		t.Fatal("flushed key must be cleared from Pending")
	}
}

func TestBatcher_FlushesImmediatelyAtMaxBatchSize(t *testing.T) {
	b := notify.NewBatcher(time.Hour, 3)
	now := time.Now()

	b.Add(p2Event("c1", "wf1", "e1", "d1"), now)
	b.Add(p2Event("c1", "wf1", "e2", "d2"), now)
	digest, ready := b.Add(p2Event("c1", "wf1", "e3", "d3"), now)
	if !ready {
		t.Fatal("reaching maxBatchSize must flush immediately regardless of window")
	}
	if digest.EventsCount != 3 {
		t.Fatalf("want 3 events in the immediate-flush digest, got %d", digest.EventsCount)
	}
}

func TestBatcher_SeparateAggregationKeysDoNotMix(t *testing.T) {
	b := notify.NewBatcher(time.Minute, 100)
	now := time.Now()

	b.Add(p2Event("c1", "wf1", "e1", "d1"), now)
	b.Add(p2Event("c1", "wf2", "e2", "d2"), now) // different workflow, same chat
	b.Add(p2Event("c2", "wf1", "e3", "d3"), now) // different chat, same workflow

	if got := b.Pending(); got != 3 {
		t.Fatalf("want 3 independent aggregation keys, got %d", got)
	}

	digests := b.Tick(now.Add(2 * time.Minute))
	if len(digests) != 3 {
		t.Fatalf("want 3 separate digests, got %d", len(digests))
	}
	for _, d := range digests {
		if d.EventsCount != 1 {
			t.Fatalf("each separate key must flush its own single event, got %+v", d)
		}
	}
}
