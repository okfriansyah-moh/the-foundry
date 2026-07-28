package evolve

import "testing"

func TestLearningLaneAdmit(t *testing.T) {
	lane := LearningLane{HeadroomThreshold: 0.2}
	cases := []struct {
		name   string
		cap    Capacity
		wantOK bool
	}{
		{"idle admits", Capacity{DeliveryLoad: 0.1, ProviderBudgetHeadroom: 1.0}, true},
		{"saturated denies", Capacity{DeliveryLoad: 0.95, ProviderBudgetHeadroom: 1.0}, false},
		{"low budget denies", Capacity{DeliveryLoad: 0.1, ProviderBudgetHeadroom: 0.1}, false},
		{"brownout denies", Capacity{DeliveryLoad: 0.0, ProviderBudgetHeadroom: 1.0, Brownout: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := lane.Admit(tc.cap)
			if ok != tc.wantOK {
				t.Fatalf("Admit=%v (%q), want %v", ok, reason, tc.wantOK)
			}
			if !ok && reason == "" {
				t.Fatal("denied admission must carry a reason")
			}
		})
	}
}

// TestSoak_DeliveryUnaffectedByLearning is the Task 82 acceptance at unit
// scale: delivery p95 is unchanged (±5%) whether or not the learning lane is
// saturated, and delivery is never starved (all delivery tasks complete),
// while learning may be starved (acceptable).
func TestSoak_DeliveryUnaffectedByLearning(t *testing.T) {
	arrivals := make([]int, 200)
	for i := range arrivals {
		if i%2 == 0 {
			arrivals[i] = 1
		}
	}
	base := SoakConfig{
		Workers: 4, Ticks: 220, DeliveryArrivals: arrivals, DeliveryDuration: 3,
		Lane: LearningLane{HeadroomThreshold: 0.2}, BudgetHeadroom: 1.0,
	}
	deliveryOnly := RunSoak(base)

	withLearning := base
	withLearning.LearningSaturated = true
	learn := RunSoak(withLearning)

	if deliveryOnly.DeliveryCompleted != learn.DeliveryCompleted {
		t.Fatalf("delivery completion changed under learning load: %d vs %d",
			deliveryOnly.DeliveryCompleted, learn.DeliveryCompleted)
	}
	// Delivery p95 within ±5%.
	lo := float64(deliveryOnly.DeliveryP95) * 0.95
	hi := float64(deliveryOnly.DeliveryP95)*1.05 + 1 // +1 tolerance for integer p95
	if float64(learn.DeliveryP95) < lo || float64(learn.DeliveryP95) > hi {
		t.Fatalf("delivery p95 shifted under learning: base=%d learn=%d (bound [%.1f,%.1f])",
			deliveryOnly.DeliveryP95, learn.DeliveryP95, lo, hi)
	}
	// Learning must have used SOME leftover capacity (not fully starved when
	// delivery has idle ticks).
	if learn.LearningAdmitted == 0 {
		t.Fatal("learning got zero leftover capacity — expected some admission during idle ticks")
	}
}

// TestSoak_DeliverySaturatedStarvesLearning proves the priority: when delivery
// keeps every worker busy, learning is starved (acceptable) — delivery is
// never displaced.
func TestSoak_DeliverySaturatedStarvesLearning(t *testing.T) {
	arrivals := make([]int, 100)
	for i := range arrivals {
		arrivals[i] = 4 // flood: fills all 4 workers every tick
	}
	cfg := SoakConfig{
		Workers: 4, Ticks: 100, DeliveryArrivals: arrivals, DeliveryDuration: 2,
		LearningSaturated: true, Lane: LearningLane{HeadroomThreshold: 0.5}, BudgetHeadroom: 1.0,
	}
	res := RunSoak(cfg)
	if res.LearningAdmitted != 0 {
		t.Fatalf("saturated delivery must starve learning, but %d learning tasks ran", res.LearningAdmitted)
	}
}
