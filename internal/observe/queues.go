package observe

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Lane is one of the four worker-task-queue priority classes docs/PLAN.md
// Task 33's card names, in the fixed order recovery > delivery >
// notification > learning (highest priority first).
type Lane string

const (
	LaneRecovery     Lane = "recovery"
	LaneDelivery     Lane = "delivery"
	LaneNotification Lane = "notification"
	LaneLearning     Lane = "learning"
)

// laneOrder is the card's own literal priority order. QueueConfig
// validation rejects any config/queue-priority.yaml whose lanes don't
// name exactly these four, in this exact priority order — config drift
// here would silently invert which lane a brownout sheds first.
var laneOrder = []Lane{LaneRecovery, LaneDelivery, LaneNotification, LaneLearning}

// LaneConfig is one config/queue-priority.yaml lane entry.
type LaneConfig struct {
	Name        string `yaml:"name"`
	TaskQueue   string `yaml:"task_queue"`
	Priority    int    `yaml:"priority"`
	WorkerSlots int    `yaml:"worker_slots"`
	Sheddable   bool   `yaml:"sheddable"`
}

// QueueConfig is config/queue-priority.yaml's parsed shape: the priority
// lane -> Temporal task queue name + worker slot allocation mapping this
// task's card requires ("separate Temporal task queues + worker slot
// allocation").
//
// decision (no-gaps rule): this task's Outputs name "queue config" but not
// cmd/foundryd/main.go or internal/kernel/workflow.go, and go-backend's
// own authority boundary (.ai/agents/go-backend/AGENT.md) never touches
// internal/kernel — routing a specific DeliverPlan execution onto one of
// these four task queues is a sequencing decision Constitution C4 reserves
// to the kernel. This type is therefore the declarative config + lookup
// surface only; wiring a real per-lane worker.New() call in cmd/foundryd
// (and whatever decides a workflow's lane at start time) is left to a
// future go-kernel-owned task, the same smallest-reversible interpretation
// Task 32's Status line already used for ProjectionSource/WorkflowController.
type QueueConfig struct {
	Lanes []LaneConfig `yaml:"lanes"`
}

// LoadQueueConfig reads and validates a config/queue-priority.yaml-shaped
// file: exactly the four lanes in laneOrder, contiguous zero-based
// priorities matching that order, positive worker_slots, and non-empty
// task_queue names.
func LoadQueueConfig(path string) (QueueConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return QueueConfig{}, fmt.Errorf("observe: read queue config %s: %w", path, err)
	}
	var cfg QueueConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return QueueConfig{}, fmt.Errorf("observe: parse queue config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return QueueConfig{}, fmt.Errorf("observe: queue config %s: %w", path, err)
	}
	return cfg, nil
}

func (c QueueConfig) validate() error {
	if len(c.Lanes) != len(laneOrder) {
		return fmt.Errorf("expected exactly %d lanes, got %d", len(laneOrder), len(c.Lanes))
	}
	for i, want := range laneOrder {
		got := c.Lanes[i]
		if got.Name != string(want) {
			return fmt.Errorf("lane %d: expected name %q (fixed priority order recovery>delivery>notification>learning), got %q", i, want, got.Name)
		}
		if got.Priority != i {
			return fmt.Errorf("lane %q: expected priority %d matching its position in the fixed order, got %d", got.Name, i, got.Priority)
		}
		if got.TaskQueue == "" {
			return fmt.Errorf("lane %q: task_queue must not be empty", got.Name)
		}
		if got.WorkerSlots <= 0 {
			return fmt.Errorf("lane %q: worker_slots must be positive, got %d", got.Name, got.WorkerSlots)
		}
	}
	return nil
}

// Lane returns cfg's entry for name, or ok=false if name isn't one of
// the four configured lanes.
func (c QueueConfig) Lane(name Lane) (LaneConfig, bool) {
	for _, l := range c.Lanes {
		if l.Name == string(name) {
			return l, true
		}
	}
	return LaneConfig{}, false
}
