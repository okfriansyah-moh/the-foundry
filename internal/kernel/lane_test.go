package kernel_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// repoQueueConfigPath mirrors internal/observe/queues_test.go's own
// relative path to the repo's real config/queue-priority.yaml — Task 96's
// golden cases run against the actual four-lane config, not a synthetic
// fixture, so a drift in that file's lane names would fail here too.
const repoQueueConfigPath = "../../config/queue-priority.yaml"

// TestLaneSelector_Select is docs/PLAN.md Task 96's required golden-case
// trio: allowed-explicit / denied-explicit / no-explicit-uses-default.
func TestLaneSelector_Select(t *testing.T) {
	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig(%s): %v", repoQueueConfigPath, err)
	}

	tests := []struct {
		name         string
		explicitLane string
		wantQueue    string
		wantErr      bool
	}{
		{
			name:         "allowed-explicit",
			explicitLane: "recovery",
			wantQueue:    "foundry-recovery",
		},
		{
			name:         "denied-explicit",
			explicitLane: "nonexistent-lane",
			wantErr:      true,
		},
		{
			name:         "no-explicit-uses-default",
			explicitLane: "",
			wantQueue:    "foundry-delivery",
		},
	}

	var sel kernel.LaneSelector
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sel.Select(tt.explicitLane, cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Select(%q) = %q, nil; want an error (fail-closed on an unconfigured lane)", tt.explicitLane, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Select(%q): unexpected error: %v", tt.explicitLane, err)
			}
			if got != tt.wantQueue {
				t.Errorf("Select(%q) = %q, want %q", tt.explicitLane, got, tt.wantQueue)
			}
		})
	}
}

// TestLaneSelector_Select_AllFourLanesResolve proves every one of the
// repo config's four declared lanes round-trips through Select — the
// same four lanes cmd/foundryd/main.go must register one worker each for.
func TestLaneSelector_Select_AllFourLanesResolve(t *testing.T) {
	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig(%s): %v", repoQueueConfigPath, err)
	}

	var sel kernel.LaneSelector
	for _, l := range cfg.Lanes {
		got, err := sel.Select(l.Name, cfg)
		if err != nil {
			t.Errorf("Select(%q): unexpected error: %v", l.Name, err)
			continue
		}
		if got != l.TaskQueue {
			t.Errorf("Select(%q) = %q, want %q", l.Name, got, l.TaskQueue)
		}
	}
}
