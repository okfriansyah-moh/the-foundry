package retention

import "testing"

func TestCascadeTargets(t *testing.T) {
	registry := Registry{"customer_data": {Name: "customer_data", Cascade: []string{"memory_rows", "caches"}}}
	targets := CascadeTargets(registry, "customer_data")
	if len(targets) != 2 {
		t.Fatalf("targets=%v", targets)
	}
}
