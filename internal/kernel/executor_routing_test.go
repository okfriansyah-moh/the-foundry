package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

func routingRegistry() capability.Registry {
	return capability.Registry{Executors: []capability.Record{
		{Provider: "claude-code", Availability: capability.AvailabilitySupported},
		{Provider: "opencode", Availability: capability.AvailabilitySupported},
		{Provider: "cursor", Availability: capability.AvailabilitySupported},
		{Provider: "copilot", Availability: capability.AvailabilitySupported},
	}}
}

func routingTable() RoutingTable {
	return RoutingTable{
		"architecture": {"claude-code", "opencode"},
		"frontend":     {"cursor", "opencode"},
		"review":       {"copilot", "claude-code"},
	}
}

// TestExecutorSelect_Routing exercises Task 90's task-class routing.
func TestExecutorSelect_Routing(t *testing.T) {
	reg := routingRegistry()
	cases := []struct {
		name       string
		selector   ExecutorSelector
		task       plan.Task
		allow      []string
		wantName   string
		wantReason string
	}{
		{
			name:     "class routed to first preference",
			selector: ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:     plan.Task{Class: "frontend"},
			allow:    []string{"cursor", "opencode", "claude-code"},
			wantName: "cursor",
		},
		{
			name:     "first preference not allowlisted falls to second",
			selector: ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:     plan.Task{Class: "frontend"},
			allow:    []string{"opencode", "claude-code"}, // cursor denied
			wantName: "opencode",
		},
		{
			name:     "explicit executor overrides routing",
			selector: ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:     plan.Task{Class: "frontend", Executor: "claude-code"},
			allow:    []string{"cursor", "claude-code"},
			wantName: "claude-code",
		},
		{
			name:     "unclassed task uses default even when routing active",
			selector: ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:     plan.Task{},
			allow:    []string{"claude-code"},
			wantName: "claude-code",
		},
		{
			name:       "classed but unrouted class fails closed",
			selector:   ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:       plan.Task{Class: "quantum-computing"},
			allow:      []string{"claude-code"},
			wantReason: ReasonUnroutedClass,
		},
		{
			name:       "routed class with no allowlisted preference fails closed",
			selector:   ExecutorSelector{Default: "claude-code", Routing: routingTable()},
			task:       plan.Task{Class: "review"},
			allow:      []string{"opencode"}, // neither copilot nor claude-code allowed
			wantReason: ReasonNoEligibleExecutor,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.selector.Select(context.Background(), tc.task, resolvedWith(tc.allow...), reg)
			if tc.wantReason == "" {
				if err != nil {
					t.Fatalf("Select: unexpected error %v", err)
				}
				if got != tc.wantName {
					t.Fatalf("Select = %q, want %q", got, tc.wantName)
				}
				return
			}
			if err == nil {
				t.Fatalf("Select = %q, want fail-closed reason %q", got, tc.wantReason)
			}
			var se *SelectionError
			if !errors.As(err, &se) {
				t.Fatalf("error is not *SelectionError: %v", err)
			}
			if se.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", se.Reason, tc.wantReason)
			}
		})
	}
}

// TestExecutorSelect_Routing_RespectsEligibility proves an unsupported
// registry record is never routed to, even if listed and allowlisted.
func TestExecutorSelect_Routing_RespectsEligibility(t *testing.T) {
	reg := capability.Registry{Executors: []capability.Record{
		{Provider: "cursor", Availability: capability.AvailabilityUnsupported},
		{Provider: "opencode", Availability: capability.AvailabilitySupported},
	}}
	s := ExecutorSelector{Default: "opencode", Routing: RoutingTable{"frontend": {"cursor", "opencode"}}}
	got, err := s.Select(context.Background(), plan.Task{Class: "frontend"}, resolvedWith("cursor", "opencode"), reg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got != "opencode" {
		t.Fatalf("Select = %q, want opencode (cursor unsupported must be skipped)", got)
	}
}

// TestLoadRoutingTable loads the shipped config and a strict-rejection case.
func TestLoadRoutingTable(t *testing.T) {
	rt, err := LoadRoutingTable(filepath.Join("..", "..", "config", "executor-routing.yaml"))
	if err != nil {
		t.Fatalf("LoadRoutingTable(shipped): %v", err)
	}
	if len(rt["frontend"]) == 0 {
		t.Fatal("shipped routing table missing 'frontend' class")
	}

	// Absent file must yield an empty (inactive) table, not an error.
	absent := filepath.Join(t.TempDir(), "no-such.yaml")
	rtEmpty, err := LoadRoutingTable(absent)
	if err != nil {
		t.Fatalf("absent routing table must be non-fatal, got: %v", err)
	}
	if len(rtEmpty) != 0 {
		t.Fatal("absent routing table must yield empty table")
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("routes:\n  frontend: [cursor]\nbogus: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRoutingTable(bad); err == nil {
		t.Fatal("expected strict-rejection error for unknown top-level key")
	}
}
