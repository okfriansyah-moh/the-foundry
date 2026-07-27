package notify

import (
	"context"
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// EngineAlerter adapts an *Engine to satisfy internal/observe.Alerter,
// reusing Task 30's notification engine for Task 33's dead-letter P1
// alert rather than duplicating it (docs/PLAN.md Task 33 Steps: "dead-
// letter table + P1 alert"). internal/observe cannot import this package
// directly — notify already imports observe for the queue_depth metric
// (Task 31), so the reverse import would be a cycle — so observe.Alerter
// is a minimal interface this package satisfies instead.
type EngineAlerter struct {
	Engine   *Engine
	ChatID   string
	ChatType ChatType
}

// Alert ingests a.Reason as a P1Command event addressed to a.ChatID —
// P1's Class.Immediate() means it is sent as its own dedicated message,
// never coalesced into a P2/P3 digest, matching this task's Steps.
func (a EngineAlerter) Alert(ctx context.Context, alert observe.DeadLetterAlert) error {
	ev := Event{
		Class:     P1Command,
		ChatID:    a.ChatID,
		ChatType:  a.ChatType,
		Text:      fmt.Sprintf("dead-letter alert: queue=%s item=%s reason=%s", alert.Queue, alert.ItemID, alert.Reason),
		DedupeKey: "dead-letter:" + alert.Queue + ":" + alert.ItemID,
	}
	return a.Engine.Ingest(ctx, ev)
}
