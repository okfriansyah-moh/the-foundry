package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Checkpoint is the operator-visible summary of a DeliverPlan workflow's
// durable progress at the moment one internal/state.Transition is
// recorded: the last plan task that finished and every evidence bundle
// recorded up to and including it. It is never persisted by this
// package — Temporal's own history and internal/kernel's PG-backed
// receipts/transitions stores are the actual durable state (docs/PLAN.md
// Task 16 Step 1).
type Checkpoint struct {
	// LastCompletedTaskID is the ID of the most recent plan task whose
	// result was fully recorded (evidence written, transition-worthy).
	// Empty before any task has completed.
	LastCompletedTaskID string
	// EvidenceIDs are the evidence.Store bundle IDs recorded so far, in
	// completion order.
	EvidenceIDs []string
}

// ID derives a deterministic, content-addressed CheckpointID from c: the
// same (LastCompletedTaskID, EvidenceIDs) pair always yields the same ID,
// regardless of process, and the order EvidenceIDs happen to be supplied
// in never changes it (they are sorted before hashing). It is a
// fingerprint for humans comparing two transitions at a glance — not a
// lookup key into any store, and computing it performs no I/O, so it is
// safe to call from deterministic Temporal workflow code.
func (c Checkpoint) ID() string {
	sorted := append([]string(nil), c.EvidenceIDs...)
	sort.Strings(sorted)

	h := sha256.New()
	h.Write([]byte(c.LastCompletedTaskID))
	h.Write([]byte{'|'})
	h.Write([]byte(strings.Join(sorted, ",")))

	return "checkpoint-" + hex.EncodeToString(h.Sum(nil))[:16]
}
