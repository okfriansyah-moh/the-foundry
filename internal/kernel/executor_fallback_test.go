package kernel_test

import (
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

func fallbackRegistry() capability.Registry {
	return capability.Registry{Executors: []capability.Record{
		{Provider: "prov-a", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
		{Provider: "prov-b", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
		{Provider: "prov-healthy-not-allowed", ExecutionClass: "cli-agentic", Availability: capability.AvailabilitySupported, LastVerifiedAt: time.Now()},
	}}
}

func resolved(allow ...string) compiler.Resolved {
	return compiler.Resolved{Effective: compiler.Policy{ExecutorAllowlist: allow}}
}

// TestSelectCandidates_OrderedAllowlistConstrained proves docs/PLAN.md Task 129:
// SelectCandidates returns the routed preference order, filtered to allowlisted
// + supported providers — a healthy provider OUTSIDE the allowlist is never a
// candidate.
func TestSelectCandidates_OrderedAllowlistConstrained(t *testing.T) {
	sel := kernel.ExecutorSelector{
		Routing: kernel.RoutingTable{"backend": {"prov-a", "prov-b", "prov-healthy-not-allowed"}},
		Profile: "personal",
	}
	task := plan.Task{ID: "t1", Class: "backend"}
	cands, skipped, err := sel.SelectCandidates(task, resolved("prov-a", "prov-b"), fallbackRegistry(), capability.HealthSnapshot{})
	if err != nil {
		t.Fatalf("SelectCandidates: %v", err)
	}
	if len(cands) != 2 || cands[0] != "prov-a" || cands[1] != "prov-b" {
		t.Fatalf("candidates = %v, want [prov-a prov-b] in order", cands)
	}
	if len(skipped) != 0 {
		t.Fatalf("no provider should be health-skipped here, got %v", skipped)
	}
	for _, c := range cands {
		if c == "prov-healthy-not-allowed" {
			t.Fatal("a healthy provider outside the allowlist must never be selected")
		}
	}
}

// TestSelectCandidates_HealthFiltered proves a tripped provider is skipped and
// the next allowed candidate is offered.
func TestSelectCandidates_HealthFiltered(t *testing.T) {
	sel := kernel.ExecutorSelector{Routing: kernel.RoutingTable{"backend": {"prov-a", "prov-b"}}, Profile: "personal"}
	h := capability.NewHealthTracker()
	h.FailureThreshold = 1
	now := time.Unix(1_700_000_000, 0)
	h.RecordFailure("prov-a", now) // trips prov-a

	task := plan.Task{ID: "t1", Class: "backend"}
	cands, skipped, err := sel.SelectCandidates(task, resolved("prov-a", "prov-b"), fallbackRegistry(), h.Snapshot(now))
	if err != nil {
		t.Fatalf("SelectCandidates: %v", err)
	}
	if len(cands) != 1 || cands[0] != "prov-b" {
		t.Fatalf("candidates = %v, want [prov-b] (prov-a tripped)", cands)
	}
	if len(skipped) != 1 || skipped[0] != "prov-a" {
		t.Fatalf("skipped = %v, want [prov-a]", skipped)
	}
}

// TestSelectCandidates_ExhaustionFailsClosed proves that when every candidate is
// tripped, selection fails closed with no-eligible-executor and a skip list.
func TestSelectCandidates_ExhaustionFailsClosed(t *testing.T) {
	sel := kernel.ExecutorSelector{Routing: kernel.RoutingTable{"backend": {"prov-a", "prov-b"}}, Profile: "personal"}
	h := capability.NewHealthTracker()
	h.FailureThreshold = 1
	now := time.Unix(1_700_000_000, 0)
	h.RecordFailure("prov-a", now)
	h.RecordFailure("prov-b", now)

	task := plan.Task{ID: "t1", Class: "backend"}
	_, skipped, err := sel.SelectCandidates(task, resolved("prov-a", "prov-b"), fallbackRegistry(), h.Snapshot(now))
	var se *kernel.SelectionError
	if !errors.As(err, &se) || se.Reason != kernel.ReasonNoEligibleExecutor {
		t.Fatalf("want no-eligible-executor SelectionError, got %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("skip list = %v, want both tripped providers for diagnosability", skipped)
	}
}
