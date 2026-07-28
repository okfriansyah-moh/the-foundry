package evolve

import "sort"

// Capacity is the instantaneous system-capacity signal the learning lane
// scheduler reads (docs/PLAN.md Task 82 / EVO-09). It is sourced from provider
// budgets and worker saturation (Task 31) and the brownout controller (Task
// 33).
type Capacity struct {
	// DeliveryLoad is delivery worker utilization in [0,1] (1 = saturated).
	DeliveryLoad float64
	// ProviderBudgetHeadroom is remaining provider budget fraction in [0,1].
	ProviderBudgetHeadroom float64
	// Brownout is true when the system is shedding load (Task 33). Learning
	// is the first thing shed, so any brownout denies learning admission.
	Brownout bool
}

// LearningLane admits eval/shadow learning work ONLY above a headroom
// threshold, so learning consumes leftover capacity and never competes with
// delivery. Starvation of learning is acceptable; starvation of delivery is
// impossible (learning is never admitted when delivery needs the capacity).
type LearningLane struct {
	// HeadroomThreshold is the minimum worker headroom (1-DeliveryLoad) AND
	// provider-budget headroom required to admit learning, in [0,1].
	HeadroomThreshold float64
}

// Admit reports whether a learning task may run under c, and why not when
// denied. It fails closed: any brownout, or insufficient worker/budget
// headroom, denies admission.
func (l LearningLane) Admit(c Capacity) (bool, string) {
	if c.Brownout {
		return false, "brownout: learning shed first"
	}
	if 1-c.DeliveryLoad < l.HeadroomThreshold {
		return false, "insufficient worker headroom for learning"
	}
	if c.ProviderBudgetHeadroom < l.HeadroomThreshold {
		return false, "insufficient provider-budget headroom for learning"
	}
	return true, ""
}

// SoakConfig parameterizes RunSoak, the deterministic delivery-vs-learning
// load simulation.
type SoakConfig struct {
	Workers           int
	Ticks             int
	DeliveryArrivals  []int // delivery tasks arriving at each tick (indexed by tick)
	DeliveryDuration  int   // ticks a delivery task occupies a worker
	LearningSaturated bool  // learning always wants a slot
	Lane              LearningLane
	// BudgetHeadroom is the (constant) provider-budget headroom during the sim.
	BudgetHeadroom float64
}

// SoakResult is RunSoak's outcome.
type SoakResult struct {
	DeliveryP95       int
	DeliveryCompleted int
	LearningAdmitted  int
}

// RunSoak simulates a worker pool serving delivery FIFO with a learning lane
// competing for capacity. Learning is admitted only on headroom (Lane.Admit),
// runs a single preemptible tick, and is SHED the instant delivery needs a
// worker — so delivery is never delayed by learning. Running it with
// LearningSaturated true vs false yields identical delivery latencies, which
// is exactly the p95-unchanged acceptance (docs/PLAN.md Task 82).
func RunSoak(cfg SoakConfig) SoakResult {
	type slot struct {
		deliveryUntil int  // tick until which a delivery occupies this worker (0 = not delivering)
		learning      bool // occupied by a 1-tick learning task this tick
	}
	workers := make([]slot, cfg.Workers)
	var queue []int // arrival ticks of waiting delivery tasks (FIFO)
	var latencies []int
	learningAdmitted := 0
	completions := 0 // counts tasks that actually finish within the simulation

	for tick := 0; tick < cfg.Ticks; tick++ {
		// 1) Free workers whose delivery FINISHED this tick; count completions
		// only at actual finish — not at assignment — so in-flight tasks
		// (deliveryUntil > last tick) are never over-counted.
		for i := range workers {
			if workers[i].deliveryUntil != 0 && workers[i].deliveryUntil <= tick {
				workers[i].deliveryUntil = 0
				completions++
			}
			workers[i].learning = false
		}
		// 2) New delivery arrivals join the queue.
		if tick < len(cfg.DeliveryArrivals) {
			for j := 0; j < cfg.DeliveryArrivals[tick]; j++ {
				queue = append(queue, tick)
			}
		}
		// 3) Assign free workers to delivery FIFO. Learning is preemptible, so
		//    a worker doing learning is already free for delivery (learning is
		//    only ever a single tick and shed here).
		for i := range workers {
			if len(queue) == 0 {
				break
			}
			if workers[i].deliveryUntil == 0 {
				arrive := queue[0]
				queue = queue[1:]
				workers[i].deliveryUntil = tick + cfg.DeliveryDuration
				latencies = append(latencies, tick-arrive) // wait latency (queue time)
			}
		}
		// 4) Delivery load = fraction of workers busy on delivery this tick.
		busy := 0
		for i := range workers {
			if workers[i].deliveryUntil != 0 {
				busy++
			}
		}
		load := float64(busy) / float64(cfg.Workers)
		// 5) Learning fills only genuinely-idle workers, gated by Admit. It
		//    never runs when delivery is queued (delivery was served first).
		if cfg.LearningSaturated && len(queue) == 0 {
			capSig := Capacity{DeliveryLoad: load, ProviderBudgetHeadroom: cfg.BudgetHeadroom}
			if ok, _ := cfg.Lane.Admit(capSig); ok {
				for i := range workers {
					if workers[i].deliveryUntil == 0 {
						workers[i].learning = true
						learningAdmitted++
						break // one learning task per tick keeps it strictly leftover
					}
				}
			}
		}
	}

	return SoakResult{
		DeliveryP95:       p95(latencies),
		DeliveryCompleted: completions,
		LearningAdmitted:  learningAdmitted,
	}
}

func p95(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]int, len(xs))
	copy(cp, xs)
	sort.Ints(cp)
	idx := (len(cp) * 95) / 100
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}
