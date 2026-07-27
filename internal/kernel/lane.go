package kernel

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

// defaultLane is the lane a DeliverPlan execution starts on when its
// caller names no explicit lane (docs/PLAN.md Task 96's card: "defaults
// to delivery when unset").
const defaultLane = observe.LaneDelivery

// LaneSelector is Task 96's kernel-owned sequencing decision (Constitution
// C4: "Kernel owns sequencing ... policy"): which of
// config/queue-priority.yaml's four declared lanes' Temporal task queue a
// DeliverPlan execution starts on. Select's only inputs are an explicit
// lane name supplied by its caller and the parsed queue config — a
// deterministic config lookup, never a PEC proposal or any other LLM
// output (Constitution C5: PEC only proposes; it never selects a lane).
type LaneSelector struct{}

// Select resolves explicitLane to its configured Temporal task queue
// name. An empty explicitLane resolves to defaultLane ("delivery").  Any
// non-empty explicitLane that does not name one of cfg's four configured
// lanes fails closed: Select returns an error rather than silently
// falling back to the default, because guessing a queue for an
// unrecognized lane name would itself be an undeclared sequencing
// decision.
func (LaneSelector) Select(explicitLane string, cfg observe.QueueConfig) (string, error) {
	lane := observe.Lane(explicitLane)
	if explicitLane == "" {
		lane = defaultLane
	}
	laneCfg, ok := cfg.Lane(lane)
	if !ok {
		return "", fmt.Errorf("kernel: lane %q is not a configured queue-priority lane", explicitLane)
	}
	return laneCfg.TaskQueue, nil
}
