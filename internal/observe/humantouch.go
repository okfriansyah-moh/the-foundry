package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TouchKind classifies a human-actionable event for Task 132's measured
// avoidable-human-touch count.
type TouchKind string

const (
	TouchBlockingGate    TouchKind = "blocking_gate"
	TouchApprovalRequest TouchKind = "approval_request"
	TouchManualCommand   TouchKind = "manual_command"
	TouchUnavoidable     TouchKind = "unavoidable_by_design"
)

// HumanTouch is one counted human-actionable event.
type HumanTouch struct {
	Kind      TouchKind `json:"kind"`
	Name      string    `json:"name"`
	At        time.Time `json:"at"`
	Avoidable bool      `json:"avoidable"`
}

// HumanTouchCounter is the machine-counted human-touch instrument.
type HumanTouchCounter struct {
	mu     sync.Mutex
	events []HumanTouch
}

// Record appends a touch. Unavoidable touches (readiness ceremony, legitimate
// H-tier approval) set Avoidable=false and are named individually.
func (c *HumanTouchCounter) Record(kind TouchKind, name string, avoidable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, HumanTouch{
		Kind:      kind,
		Name:      name,
		At:        time.Now().UTC(),
		Avoidable: avoidable,
	})
}

// AvoidableCount returns the measured avoidable-touch total.
func (c *HumanTouchCounter) AvoidableCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Avoidable {
			n++
		}
	}
	return n
}

// Snapshot returns a copy of all recorded touches.
func (c *HumanTouchCounter) Snapshot() []HumanTouch {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]HumanTouch, len(c.events))
	copy(out, c.events)
	return out
}

// WriteEvidence writes the touch ledger into an evidence bundle directory.
func (c *HumanTouchCounter) WriteEvidence(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	type report struct {
		AvoidableCount   int          `json:"avoidable_count"`
		UnavoidableNamed []HumanTouch `json:"unavoidable_named"`
		All              []HumanTouch `json:"all"`
	}
	snap := c.Snapshot()
	var unavoidable []HumanTouch
	avoidable := 0
	for _, e := range snap {
		if e.Avoidable {
			avoidable++
		} else {
			unavoidable = append(unavoidable, e)
		}
	}
	raw, err := json.MarshalIndent(report{
		AvoidableCount:   avoidable,
		UnavoidableNamed: unavoidable,
		All:              snap,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "human-touches.json"), raw, 0o644)
}
