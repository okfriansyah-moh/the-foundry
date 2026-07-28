// Command learning is the capacity-aware learning soak harness for
// docs/PLAN.md Task 82 (EVO-09), run by `make soak-learning`. It drives the
// real internal/evolve delivery-vs-learning simulation and fails if
// saturating the learning lane shifts delivery p95 by more than ±5% or if any
// delivery task is starved.
package main

import (
	"fmt"
	"log"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

func main() {
	arrivals := make([]int, 1000)
	for i := range arrivals {
		if i%2 == 0 {
			arrivals[i] = 1
		}
	}
	base := evolve.SoakConfig{
		// Ticks = arrivals_len + DeliveryDuration drain window so all in-flight
		// tasks finish within the simulation (Copilot review finding).
		Workers: 8, Ticks: 1010, DeliveryArrivals: arrivals, DeliveryDuration: 4,
		Lane: evolve.LearningLane{HeadroomThreshold: 0.2}, BudgetHeadroom: 1.0,
	}
	deliveryOnly := evolve.RunSoak(base)

	withLearning := base
	withLearning.LearningSaturated = true
	learn := evolve.RunSoak(withLearning)

	fmt.Printf("soak-learning: delivery-only  p95=%d completed=%d\n", deliveryOnly.DeliveryP95, deliveryOnly.DeliveryCompleted)
	fmt.Printf("soak-learning: with-learning  p95=%d completed=%d learning_admitted=%d\n", learn.DeliveryP95, learn.DeliveryCompleted, learn.LearningAdmitted)

	if learn.DeliveryCompleted != deliveryOnly.DeliveryCompleted {
		log.Fatalf("soak-learning: FAIL — delivery completion changed under learning load (%d vs %d)",
			deliveryOnly.DeliveryCompleted, learn.DeliveryCompleted)
	}
	lo := float64(deliveryOnly.DeliveryP95) * 0.95
	hi := float64(deliveryOnly.DeliveryP95)*1.05 + 1
	if float64(learn.DeliveryP95) < lo || float64(learn.DeliveryP95) > hi {
		log.Fatalf("soak-learning: FAIL — delivery p95 shifted beyond ±5%% (base=%d learn=%d)",
			deliveryOnly.DeliveryP95, learn.DeliveryP95)
	}
	if learn.LearningAdmitted == 0 {
		log.Fatalf("soak-learning: FAIL — learning never used leftover capacity")
	}
	fmt.Println("soak-learning: PASS — delivery p95 unchanged (±5%) with learning lane saturated")
}
