package cost

import "testing"

func TestDetectVariance(t *testing.T) {
	variance := DetectVariance(10, 12.5, 1)
	if !variance.Exceeds || variance.DeltaUSD != 2.5 {
		t.Fatalf("variance=%+v", variance)
	}
}
