package state

import (
	"errors"
	"fmt"
	"time"
)

// Transition is the canonical transition record (state-model.md §3, "Transition
// records use canonical fields").
type Transition struct {
	WorkflowID   string
	Status       Status
	PhaseFrom    Phase
	PhaseTo      Phase
	Reason       Reason
	ResultCode   ResultCode
	Actor        string
	Profile      string
	Evidence     []string
	CheckpointID string
	Attempt      int
	NextAction   string
	WakeAt       *time.Time
	OccurredAt   time.Time
}

// legalTransitions is the canonical status-transition graph (state-model.md
// §1, §4): PENDING→RUNNING; RUNNING⇄WAITING; RUNNING→{SUCCEEDED,FAILED,
// CANCELLED}; WAITING→{RUNNING,CANCELLED,FAILED}. Terminal statuses
// (SUCCEEDED, FAILED, CANCELLED) absorb — no transitions out.
var legalTransitions = map[Status]map[Status]bool{
	StatusPending: {
		StatusRunning: true,
	},
	StatusRunning: {
		StatusWaiting:   true,
		StatusSucceeded: true,
		StatusFailed:    true,
		StatusCancelled: true,
	},
	StatusWaiting: {
		StatusRunning:   true,
		StatusCancelled: true,
		StatusFailed:    true,
	},
}

// ErrIllegalTransition is returned when a from→to status pair is not in the
// canonical transition graph.
var ErrIllegalTransition = errors.New("state: illegal status transition")

// ErrInvariantViolation is returned when a Transition's fields violate a
// state-model invariant for its target status.
var ErrInvariantViolation = errors.New("state: transition invariant violation")

// Validate checks that transitioning from `from` to `to` is legal per the
// canonical graph, and that t's fields satisfy the state-model invariants for
// `to`:
//
//   - WAITING requires a Reason.
//   - SUCCEEDED forbids a Reason.
//   - a set ResultCode must be a registry-known code whose registered status
//     equals `to` (this covers "FAILED with a result requires a
//     registry-known code").
func (t Transition) Validate(from, to Status) error {
	if from.IsTerminal() {
		return fmt.Errorf("%w: %s is terminal, no transitions out (attempted %s->%s)", ErrIllegalTransition, from, from, to)
	}
	if !legalTransitions[from][to] {
		return fmt.Errorf("%w: %s->%s", ErrIllegalTransition, from, to)
	}

	switch to {
	case StatusWaiting:
		if t.Reason == "" {
			return fmt.Errorf("%w: WAITING requires a Reason", ErrInvariantViolation)
		}
	case StatusSucceeded:
		if t.Reason != "" {
			return fmt.Errorf("%w: SUCCEEDED forbids a Reason (got %q)", ErrInvariantViolation, t.Reason)
		}
	}

	if t.ResultCode != "" {
		status, known := KnownResultCode(t.ResultCode)
		if !known {
			return fmt.Errorf("%w: result code %q is not registry-known", ErrInvariantViolation, t.ResultCode)
		}
		if status != to {
			return fmt.Errorf("%w: result code %q is registered on %s, not %s", ErrInvariantViolation, t.ResultCode, status, to)
		}
	}

	return nil
}
