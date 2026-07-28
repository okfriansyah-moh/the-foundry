package evolve

import "testing"

func TestTunableRegistry_InBounds(t *testing.T) {
	registry := TunableRegistry{Tunables: []Tunable{{Name: "batch_size", Min: 1, Max: 10}}}
	if !registry.InBounds("batch_size", 5) || registry.InBounds("batch_size", 20) {
		t.Fatal("unexpected bounds result")
	}
}
