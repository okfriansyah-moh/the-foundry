package cost

import "fmt"

// docs/PLAN.md Task 120 (COST-02): subscription (shadow) spend must be bounded,
// amortized and visible — not written with no ceiling and never reported.

// AmortizeSubscription returns the per-task amortized cost of a flat
// subscription fee over the tasks executed in the period (period fee ÷ tasks in
// period). With zero tasks the whole fee is attributed to the period rather than
// dividing by zero.
func AmortizeSubscription(periodFeeUSD float64, tasksInPeriod int) float64 {
	if tasksInPeriod <= 0 {
		return periodFeeUSD
	}
	return periodFeeUSD / float64(tasksInPeriod)
}

// ShadowLedger bounds subscription-priced (shadow) spend against a
// subscription-period ceiling so it halts execution when breached, exactly like
// metered spend, instead of being invisible.
type ShadowLedger struct {
	CeilingUSD  float64
	IncurredUSD float64
}

// Add records amortizedUSD of shadow spend and reports whether the ceiling is
// now breached (halt condition).
func (s *ShadowLedger) Add(amortizedUSD float64) (breached bool) {
	s.IncurredUSD += amortizedUSD
	return s.Breached()
}

// Breached reports whether shadow spend has reached or exceeded the ceiling.
func (s ShadowLedger) Breached() bool {
	return s.CeilingUSD > 0 && s.IncurredUSD >= s.CeilingUSD
}

// Remaining returns the shadow budget left before the ceiling halts execution.
func (s ShadowLedger) Remaining() float64 {
	if s.CeilingUSD <= 0 {
		return 0
	}
	r := s.CeilingUSD - s.IncurredUSD
	if r < 0 {
		return 0
	}
	return r
}

// String renders the shadow ledger for CLI/digest visibility.
func (s ShadowLedger) String() string {
	return fmt.Sprintf("shadow: $%.4f of $%.2f ceiling (remaining $%.4f, breached=%v)",
		s.IncurredUSD, s.CeilingUSD, s.Remaining(), s.Breached())
}
