package mission

import (
	"testing"
	"time"
)

func TestObserve_NoProgressCyclesTriggersKillCandidate(t *testing.T) {
	decide := DecideFromObservations([]Observation{
		{At: time.Now(), NoProgressCycles: 5, NetMRRUSD: 10, ActivationRate: 0.2, ConversionRate: 0.1},
	}, DecidePolicy{NoProgressCyclesForKill: 4, DeclineStreakForPivot: 3})
	if decide != DecideKillCandidate {
		t.Fatalf("decide=%s, want %s", decide, DecideKillCandidate)
	}
}

func TestObserve_DeclineStreakTriggersPivotProposal(t *testing.T) {
	now := time.Now()
	history := []Observation{
		{At: now.Add(-3 * 24 * time.Hour), NetMRRUSD: 100, ActivationRate: 0.2, ConversionRate: 0.1},
		{At: now.Add(-2 * 24 * time.Hour), NetMRRUSD: 90, ActivationRate: 0.2, ConversionRate: 0.1},
		{At: now.Add(-1 * 24 * time.Hour), NetMRRUSD: 80, ActivationRate: 0.2, ConversionRate: 0.1},
		{At: now, NetMRRUSD: 70, ActivationRate: 0.2, ConversionRate: 0.1},
	}
	decide := DecideFromObservations(history, DecidePolicy{NoProgressCyclesForKill: 8, DeclineStreakForPivot: 3})
	if decide != DecidePivot {
		t.Fatalf("decide=%s, want %s", decide, DecidePivot)
	}
}

func TestObserve_LowActivationTriggersImprove(t *testing.T) {
	decide := DecideFromObservations([]Observation{
		{At: time.Now(), NetMRRUSD: 120, ActivationRate: 0.03, ConversionRate: 0.1},
	}, DecidePolicy{NoProgressCyclesForKill: 8, DeclineStreakForPivot: 3})
	if decide != DecideImprove {
		t.Fatalf("decide=%s, want %s", decide, DecideImprove)
	}
}
