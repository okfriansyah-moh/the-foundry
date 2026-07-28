package cost

import "math"

type Variance struct {
	ExpectedUSD float64
	ObservedUSD float64
	DeltaUSD    float64
	Exceeds     bool
}

func DetectVariance(expectedUSD, observedUSD, thresholdUSD float64) Variance {
	delta := math.Abs(observedUSD - expectedUSD)
	return Variance{ExpectedUSD: expectedUSD, ObservedUSD: observedUSD, DeltaUSD: delta, Exceeds: delta > thresholdUSD}
}
