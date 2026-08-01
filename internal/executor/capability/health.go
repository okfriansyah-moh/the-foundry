package capability

import (
	"sync"
	"time"
)

// HealthTracker is a simple per-provider circuit breaker recording real
// execution outcomes (docs/PLAN.md Task 129 / INF-02). A provider that trips
// (too many consecutive failures, or an observed rate-limit / unavailability) is
// skipped for a cooldown window; after the window it is retried. The tracker is
// concurrency-safe and passed as an immutable Snapshot into the deterministic
// selector, so selection stays a pure function of its inputs.
type HealthTracker struct {
	mu sync.Mutex
	// tripUntil[provider] is the instant a tripped provider becomes eligible
	// again; a zero/absent entry means "healthy".
	tripUntil map[string]time.Time
	// consecutiveFailures[provider] counts consecutive failures toward the
	// trip threshold.
	consecutiveFailures map[string]int
	// trips[provider] counts how many times a provider has tripped (metrics).
	trips map[string]int

	// FailureThreshold is the consecutive-failure count that trips a provider.
	// 0 uses defaultFailureThreshold.
	FailureThreshold int
	// Cooldown is how long a tripped provider is skipped. 0 uses
	// defaultCooldown.
	Cooldown time.Duration
}

const (
	defaultFailureThreshold = 3
	defaultCooldown         = 60 * time.Second
)

// NewHealthTracker returns an empty tracker with default thresholds.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		tripUntil:           map[string]time.Time{},
		consecutiveFailures: map[string]int{},
		trips:               map[string]int{},
	}
}

func (h *HealthTracker) failureThreshold() int {
	if h.FailureThreshold > 0 {
		return h.FailureThreshold
	}
	return defaultFailureThreshold
}

func (h *HealthTracker) cooldown() time.Duration {
	if h.Cooldown > 0 {
		return h.Cooldown
	}
	return defaultCooldown
}

func (h *HealthTracker) ensure() {
	if h.tripUntil == nil {
		h.tripUntil = map[string]time.Time{}
		h.consecutiveFailures = map[string]int{}
		h.trips = map[string]int{}
	}
}

// RecordSuccess clears a provider's failure streak and any trip.
func (h *HealthTracker) RecordSuccess(provider string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensure()
	h.consecutiveFailures[provider] = 0
	delete(h.tripUntil, provider)
}

// RecordFailure records one failure; the provider trips once it crosses the
// consecutive-failure threshold.
func (h *HealthTracker) RecordFailure(provider string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensure()
	h.consecutiveFailures[provider]++
	if h.consecutiveFailures[provider] >= h.failureThreshold() {
		h.tripLocked(provider, now)
	}
}

// RecordRateLimited / RecordUnavailable trip the provider immediately: an
// observed rate-limit or unavailability is a definite signal, not a streak.
func (h *HealthTracker) RecordRateLimited(provider string, now time.Time) { h.tripNow(provider, now) }
func (h *HealthTracker) RecordUnavailable(provider string, now time.Time) { h.tripNow(provider, now) }

func (h *HealthTracker) tripNow(provider string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensure()
	h.tripLocked(provider, now)
}

func (h *HealthTracker) tripLocked(provider string, now time.Time) {
	h.tripUntil[provider] = now.Add(h.cooldown())
	h.trips[provider]++
}

// Trips returns how many times provider has tripped (for metrics).
func (h *HealthTracker) Trips(provider string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensure()
	return h.trips[provider]
}

// HealthSnapshot is an immutable view of which providers are currently tripped,
// captured at a single instant and passed into the deterministic selector so
// selection reproduces on replay.
type HealthSnapshot struct {
	at        time.Time
	tripUntil map[string]time.Time
}

// Snapshot captures the tracker's tripped set as of now.
func (h *HealthTracker) Snapshot(now time.Time) HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensure()
	cp := make(map[string]time.Time, len(h.tripUntil))
	for k, v := range h.tripUntil {
		cp[k] = v
	}
	return HealthSnapshot{at: now, tripUntil: cp}
}

// Available reports whether provider is eligible as of the snapshot instant: a
// provider whose cooldown has not elapsed is skipped. A zero snapshot (no
// tracker wired) treats every provider as available, preserving the pre-Task-129
// behaviour.
func (s HealthSnapshot) Available(provider string) bool {
	if s.tripUntil == nil {
		return true
	}
	until, tripped := s.tripUntil[provider]
	if !tripped {
		return true
	}
	return !s.at.Before(until)
}

// Tripped returns the providers currently skipped, for a diagnosable skip list.
func (s HealthSnapshot) Tripped() []string {
	var out []string
	for p, until := range s.tripUntil {
		if s.at.Before(until) {
			out = append(out, p)
		}
	}
	return out
}
