package notify

import "time"

// Class is the P0..P3 event priority named by docs/PLAN.md Task 30's
// Steps and detailed in docs/foundry/docs/operations/telegram.md §19.12.
type Class int

const (
	// P0Critical is a security incident, destructive rollback, or
	// credential exposure: always sent immediately, bypasses batching,
	// and is never dropped.
	P0Critical Class = iota
	// P1Command is a command-required or approval-required event: sent
	// as a dedicated immediate message (never coalesced into a digest),
	// but still subject to flood control like everything else.
	P1Command
	// P2State is a step/workflow state transition: coalesced briefly,
	// preserving every transition in the resulting digest.
	P2State
	// P3Progress is routine progress/heartbeat traffic: batched
	// aggressively.
	P3Progress
)

// String renders c for logs and digest text.
func (c Class) String() string {
	switch c {
	case P0Critical:
		return "P0"
	case P1Command:
		return "P1"
	case P2State:
		return "P2"
	case P3Progress:
		return "P3"
	default:
		return "P?"
	}
}

// Immediate reports whether c must be sent immediately rather than
// handed to the Batcher — true for P0 and P1 per Task 30's Steps
// ("P0 immediate") and the governing doc's P1 "dedicated message"
// behavior (§19.12).
func (c Class) Immediate() bool {
	return c == P0Critical || c == P1Command
}

// ChatType distinguishes the two Telegram chat flood-control profiles
// this engine enforces (docs/foundry/docs/operations/telegram.md §19.10).
type ChatType string

const (
	ChatPrivate ChatType = "private"
	ChatGroup   ChatType = "group"
)

// Event is one notification event admitted to the engine
// (docs/PLAN.md Task 30 Steps: "event model {class P0..P3, workflow,
// text, dedupe_key}").
type Event struct {
	Class      Class
	ChatID     string
	ChatType   ChatType
	Workflow   string
	Text       string
	DedupeKey  string
	OccurredAt time.Time
}
