package executor

import "fmt"

// Constructor builds a new Adapter instance. Registered executors are
// stateless per invocation — each call must return a fresh Adapter.
type Constructor func() Adapter

var registry = map[string]Constructor{}

// Register adds a named executor constructor to the registry. It panics on
// a duplicate name, since that indicates a wiring bug at init time, not a
// reachable runtime condition.
func Register(name string, ctor Constructor) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("executor: %q already registered", name))
	}
	registry[name] = ctor
}

// Get returns a fresh Adapter for the named executor, or an error if name
// was never registered.
func Get(name string) (Adapter, error) {
	ctor, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("executor: no adapter registered for %q", name)
	}
	return ctor(), nil
}
