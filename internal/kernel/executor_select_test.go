package kernel

import (
	"context"
	"errors"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	compiler "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
)

// goldenRegistry is the capability registry the selector golden corpus runs
// against: two supported providers plus one unsupported stub (mirrors
// Task 89's Kimi/Kilo rows).
func goldenRegistry() capability.Registry {
	return capability.Registry{Executors: []capability.Record{
		{Provider: "claude-code", Availability: capability.AvailabilitySupported},
		{Provider: "opencode", Availability: capability.AvailabilitySupported},
		{Provider: "kimi", Availability: capability.AvailabilityUnsupported},
	}}
}

func resolvedWith(allow ...string) compiler.Resolved {
	return compiler.Resolved{Effective: compiler.Policy{ExecutorAllowlist: allow}}
}

// TestExecutorSelect_Golden is Task 85's 5-case golden corpus (Step 4) plus
// the unsupported-executor case Task 89 depends on.
func TestExecutorSelect_Golden(t *testing.T) {
	reg := goldenRegistry()
	cases := []struct {
		name       string
		selector   ExecutorSelector
		task       plan.Task
		policy     compiler.Resolved
		wantName   string
		wantReason string // "" => success
	}{
		{
			name:     "allowed-explicit",
			selector: ExecutorSelector{Default: "claude-code"},
			task:     plan.Task{Executor: "opencode"},
			policy:   resolvedWith("claude-code", "opencode"),
			wantName: "opencode",
		},
		{
			name:       "denied-explicit",
			selector:   ExecutorSelector{Default: "claude-code"},
			task:       plan.Task{Executor: "opencode"},
			policy:     resolvedWith("claude-code"),
			wantReason: ReasonNotInAllowlist,
		},
		{
			name:     "no-explicit-uses-default",
			selector: ExecutorSelector{Default: "claude-code"},
			task:     plan.Task{},
			policy:   resolvedWith("claude-code"),
			wantName: "claude-code",
		},
		{
			name:       "default-not-in-allowlist-fails-closed",
			selector:   ExecutorSelector{Default: "claude-code"},
			task:       plan.Task{},
			policy:     resolvedWith("opencode"),
			wantReason: ReasonNotInAllowlist,
		},
		{
			name:       "unknown-executor-name-fails-closed",
			selector:   ExecutorSelector{Default: "claude-code"},
			task:       plan.Task{Executor: "ghost"},
			policy:     resolvedWith("claude-code", "ghost"), // in allowlist but not in registry
			wantReason: ReasonUnknownExecutor,
		},
		{
			name:       "unsupported-executor-fails-closed",
			selector:   ExecutorSelector{Default: "claude-code"},
			task:       plan.Task{Executor: "kimi"},
			policy:     resolvedWith("claude-code", "kimi"),
			wantReason: ReasonUnsupportedExecutor,
		},
		{
			name:       "no-default-no-explicit-fails-closed",
			selector:   ExecutorSelector{},
			task:       plan.Task{},
			policy:     resolvedWith("claude-code"),
			wantReason: ReasonNoExecutorConfigured,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.selector.Select(context.Background(), tc.task, tc.policy, reg)
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
				t.Fatalf("Select = %q, want fail-closed with reason %q", got, tc.wantReason)
			}
			var se *SelectionError
			if !errors.As(err, &se) {
				t.Fatalf("Select error is not *SelectionError: %v", err)
			}
			if se.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", se.Reason, tc.wantReason)
			}
			if se.Classification != verify.ClassificationPolicyViolation {
				t.Fatalf("Classification = %q, want policy-violation", se.Classification)
			}
		})
	}
}

// TestExecutorSelect_Deterministic proves identical inputs always give the
// identical result (Task 84/85 acceptance).
func TestExecutorSelect_Deterministic(t *testing.T) {
	s := ExecutorSelector{Default: "claude-code"}
	reg := goldenRegistry()
	pol := resolvedWith("claude-code", "opencode")
	task := plan.Task{}
	for i := 0; i < 50; i++ {
		got, err := s.Select(context.Background(), task, pol, reg)
		if err != nil || got != "claude-code" {
			t.Fatalf("non-deterministic: got %q err %v", got, err)
		}
	}
}

// TestExecutorSelect_Unimplemented proves the deferred Kimi/Kilo stubs
// (docs/PLAN.md Task 89 / PRV-06) fail CLOSED with the exact, distinguishable
// "unsupported-executor" reason — never a silent no-op or fallback. Asserts
// the precise error, not just non-nil.
func TestExecutorSelect_Unimplemented(t *testing.T) {
	reg := capability.Registry{Executors: []capability.Record{
		{Provider: "kimi", Availability: capability.AvailabilityUnsupported},
		{Provider: "kilo", Availability: capability.AvailabilityUnsupported},
	}}
	s := ExecutorSelector{Default: "kimi"}
	for _, name := range []string{"kimi", "kilo"} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Select(context.Background(), plan.Task{Executor: name}, resolvedWith(name), reg)
			if err == nil {
				t.Fatalf("Select(%q) succeeded, want fail-closed", name)
			}
			var se *SelectionError
			if !errors.As(err, &se) {
				t.Fatalf("error is not *SelectionError: %v", err)
			}
			if se.Reason != ReasonUnsupportedExecutor {
				t.Fatalf("Reason = %q, want %q", se.Reason, ReasonUnsupportedExecutor)
			}
			if se.Classification != verify.ClassificationPolicyViolation {
				t.Fatalf("Classification = %q, want policy-violation", se.Classification)
			}
			if se.Executor != name {
				t.Fatalf("Executor = %q, want %q", se.Executor, name)
			}
		})
	}
}
