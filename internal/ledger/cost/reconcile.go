package cost

import "fmt"

type Observation struct {
	EntryID     string
	ObservedUSD float64
}

type ReconcileResult struct {
	EntryID  string
	Variance Variance
	Released bool
}

func ReconcileEntry(entry Entry, observedUSD, thresholdUSD float64) (ReconcileResult, error) {
	if observedUSD < 0 || nonFiniteUSD(observedUSD) {
		return ReconcileResult{}, fmt.Errorf("cost: observed_usd must be a non-negative finite number, got %v", observedUSD)
	}
	variance := DetectVariance(entry.AmountUSD, observedUSD, thresholdUSD)
	return ReconcileResult{EntryID: entry.ID, Variance: variance, Released: entry.State == StateReserved && observedUSD == 0}, nil
}
