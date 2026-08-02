package cost

import (
	"fmt"
	"sync"
)

// docs/PLAN.md Task 149: unreconciled cost backlog never becomes silent zero.

// ObservationKind classifies a cost observation.
type ObservationKind string

const (
	KindObserved     ObservationKind = "observed"
	KindDerived      ObservationKind = "derived"
	KindShadowObs    ObservationKind = "shadow"
	KindUnreconciled ObservationKind = "unreconciled"
)

// BacklogEntry is one durable unreconciled/missing-usage record.
type BacklogEntry struct {
	EntryID          string
	ProfileID        string
	MissionID        string
	ProviderUsageRef string
	Kind             ObservationKind
	AmountUSD        float64
	ErrorText        string
	Attempts         int
	FreezeTriggered  bool
}

// FreezeGate reports whether unattended reservations must freeze.
type FreezeGate struct {
	mu        sync.Mutex
	frozen    map[string]bool // profile_id
	threshold int
	counts    map[string]int
}

// NewFreezeGate constructs a FreezeGate. threshold<=0 defaults to 1.
func NewFreezeGate(threshold int) *FreezeGate {
	if threshold <= 0 {
		threshold = 1
	}
	return &FreezeGate{frozen: map[string]bool{}, threshold: threshold, counts: map[string]int{}}
}

// RecordMissingUsage records missing provider usage as unreconciled (never zero)
// and freezes the profile when threshold is breached.
func (g *FreezeGate) RecordMissingUsage(profileID, entryID, errText string) BacklogEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counts[profileID]++
	e := BacklogEntry{
		EntryID:   entryID,
		ProfileID: profileID,
		Kind:      KindUnreconciled,
		ErrorText: errText,
		Attempts:  g.counts[profileID],
	}
	if g.counts[profileID] >= g.threshold {
		g.frozen[profileID] = true
		e.FreezeTriggered = true
	}
	return e
}

// AllowUnattendedReserve refuses when the profile is frozen for unreconciled costs.
func (g *FreezeGate) AllowUnattendedReserve(profileID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.frozen[profileID] {
		return fmt.Errorf("cost: unattended reservation frozen for profile %s (unreconciled backlog)", profileID)
	}
	return nil
}

// ClearAfterReconcile unfreezes only the named profile after durable correction.
func (g *FreezeGate) ClearAfterReconcile(profileID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.frozen, profileID)
	g.counts[profileID] = 0
}

// MissingUsageIsNeverZero documents the fail-closed rule for callers.
func MissingUsageIsNeverZero(observed *float64) ObservationKind {
	if observed == nil {
		return KindUnreconciled
	}
	return KindObserved
}
