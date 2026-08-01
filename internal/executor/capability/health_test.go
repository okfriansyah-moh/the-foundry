package capability_test

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
)

// TestHealthTracker_TripCooldownRecover proves docs/PLAN.md Task 129's circuit
// breaker: a provider trips after the consecutive-failure threshold, is skipped
// for the cooldown window, and recovers after it — while an observed rate-limit
// trips immediately.
func TestHealthTracker_TripCooldownRecover(t *testing.T) {
	h := capability.NewHealthTracker()
	h.FailureThreshold = 2
	h.Cooldown = time.Minute
	now := time.Unix(1_700_000_000, 0)

	// One failure does not trip.
	h.RecordFailure("prov-a", now)
	if !h.Snapshot(now).Available("prov-a") {
		t.Fatal("one failure must not trip the breaker")
	}
	// Second consecutive failure trips it.
	h.RecordFailure("prov-a", now)
	if h.Snapshot(now).Available("prov-a") {
		t.Fatal("provider must be tripped after crossing the failure threshold")
	}
	// Still skipped inside the cooldown window.
	if h.Snapshot(now.Add(30 * time.Second)).Available("prov-a") {
		t.Fatal("provider must stay skipped during cooldown")
	}
	// Available again after cooldown elapses.
	if !h.Snapshot(now.Add(90 * time.Second)).Available("prov-a") {
		t.Fatal("provider must recover after cooldown")
	}
	// A success clears the breaker immediately.
	h.RecordRateLimited("prov-b", now)
	if h.Snapshot(now).Available("prov-b") {
		t.Fatal("an observed rate-limit must trip the provider immediately")
	}
	h.RecordSuccess("prov-b")
	if !h.Snapshot(now).Available("prov-b") {
		t.Fatal("a success must clear the trip")
	}
	if h.Trips("prov-b") != 1 {
		t.Fatalf("trip counter = %d, want 1", h.Trips("prov-b"))
	}
}

// TestHealthSnapshot_ZeroValueAvailable proves a zero snapshot (no tracker
// wired) treats every provider as available, preserving pre-Task-129 behaviour.
func TestHealthSnapshot_ZeroValueAvailable(t *testing.T) {
	var s capability.HealthSnapshot
	if !s.Available("anything") {
		t.Fatal("zero-value snapshot must treat providers as available")
	}
}
