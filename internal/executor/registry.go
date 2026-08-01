package executor

import (
	"errors"
	"fmt"
)

// ErrProviderUnavailable and ErrProviderRateLimited are the typed provider-fault
// signals an adapter's Run returns when the PROVIDER (not the task) is the
// problem — unreachable/at-capacity, or rate-limited. The kernel's ExecuteTask
// falls over to the next policy-allowed executor on these, but treats any other
// Run error as the task itself failing (docs/PLAN.md Task 129 / INF-02). Wrap
// them so a caller can attach detail: fmt.Errorf("...: %w", ErrProviderUnavailable).
var (
	ErrProviderUnavailable = errors.New("executor: provider unavailable")
	ErrProviderRateLimited = errors.New("executor: provider rate-limited")
)

// IsProviderFault reports whether err signals a provider-level fault the kernel
// should fall over on (as opposed to a task failure).
func IsProviderFault(err error) bool {
	return errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrProviderRateLimited)
}

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
