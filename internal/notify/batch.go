package notify

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// aggregationKey groups events into one digest, per
// docs/foundry/docs/operations/telegram.md §19.13's aggregation_key
// (chat_id + workflow_id — this task's event model has no step_id).
type aggregationKey struct {
	ChatID   string
	ChatType ChatType
	Workflow string
}

type pendingBatch struct {
	events    []Event
	windowEnd time.Time
}

// Digest is one coalesced P2/P3 batch ready to send as a single
// Telegram message.
type Digest struct {
	ChatID      string
	ChatType    ChatType
	Workflow    string
	Text        string
	DedupeKey   string
	EventsCount int
}

// Batcher coalesces P2/P3 events into digests over a configurable
// window (docs/PLAN.md Task 30 Steps: "batcher: P2/P3 events coalesce
// into digests over a configurable window"). P0/P1 events never reach
// the batcher — Engine sends those immediately (Class.Immediate).
type Batcher struct {
	window       time.Duration
	maxBatchSize int

	mu      sync.Mutex
	batches map[aggregationKey]*pendingBatch
}

// NewBatcher constructs a Batcher that flushes a key's accumulated
// events once window has elapsed since its first event, or immediately
// once the key accumulates maxBatchSize events, whichever comes first.
func NewBatcher(window time.Duration, maxBatchSize int) *Batcher {
	if maxBatchSize < 1 {
		maxBatchSize = 1
	}
	return &Batcher{
		window:       window,
		maxBatchSize: maxBatchSize,
		batches:      make(map[aggregationKey]*pendingBatch),
	}
}

// Add accumulates ev under its aggregation key as of now. If this call
// reached maxBatchSize for that key, the coalesced Digest is returned
// immediately (ready=true) and the key's pending batch is cleared.
// Otherwise the caller must rely on Tick to flush window-elapsed keys.
func (b *Batcher) Add(ev Event, now time.Time) (digest Digest, ready bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := aggregationKey{ChatID: ev.ChatID, ChatType: ev.ChatType, Workflow: ev.Workflow}
	pb, ok := b.batches[key]
	if !ok {
		pb = &pendingBatch{windowEnd: now.Add(b.window)}
		b.batches[key] = pb
	}
	pb.events = append(pb.events, ev)

	if len(pb.events) >= b.maxBatchSize {
		delete(b.batches, key)
		return buildDigest(key, pb.events), true
	}
	return Digest{}, false
}

// Tick flushes every aggregation key whose window has elapsed as of
// now, returning one Digest per flushed key in a deterministic order.
func (b *Batcher) Tick(now time.Time) []Digest {
	b.mu.Lock()
	defer b.mu.Unlock()

	var digests []Digest
	for key, pb := range b.batches {
		if !now.Before(pb.windowEnd) {
			digests = append(digests, buildDigest(key, pb.events))
			delete(b.batches, key)
		}
	}
	sort.Slice(digests, func(i, j int) bool {
		return digests[i].DedupeKey < digests[j].DedupeKey
	})
	return digests
}

// Pending reports how many aggregation keys currently hold unflushed
// events — used by the soak test to confirm batching actually engaged.
func (b *Batcher) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.batches)
}

func buildDigest(key aggregationKey, events []Event) Digest {
	lines := make([]string, 0, len(events))
	dedupeParts := make([]string, 0, len(events))
	for _, ev := range events {
		lines = append(lines, fmt.Sprintf("%s %s", ev.Class, ev.Text))
		dedupeParts = append(dedupeParts, ev.DedupeKey)
	}
	return Digest{
		ChatID:   key.ChatID,
		ChatType: key.ChatType,
		Workflow: key.Workflow,
		Text: fmt.Sprintf("Delivery Foundry update — %s\nEvents combined: %d\n\n%s",
			key.Workflow, len(events), strings.Join(lines, "\n")),
		DedupeKey:   "digest:" + key.ChatID + ":" + key.Workflow + ":" + strings.Join(dedupeParts, ","),
		EventsCount: len(events),
	}
}
