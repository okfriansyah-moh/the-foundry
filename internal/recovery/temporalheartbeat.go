// temporalheartbeat.go: docs/PLAN.md Task 94 (FND-13R)'s live liveness
// signal for a RUNNING workflow — see supervisor.go's own doc comment.
// Postgres's workflow_status_projection has no heartbeat column at all
// (only updated_at, used elsewhere as LastProgressAt); the only real
// heartbeat signal is Temporal's own PendingActivities[].LastHeartbeatTime,
// which is why this is a distinct source composed in by composite.go
// rather than folded into postgres.go.
package recovery

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
)

// TemporalHeartbeatSource reads one workflow's freshest activity
// heartbeat straight from Temporal.
type TemporalHeartbeatSource struct {
	Client client.Client
}

// Heartbeat returns the latest LastHeartbeatTime across workflowID's
// current run's PendingActivities. If there is no pending activity (e.g.
// the workflow is between activities, or the one activity has never
// heartbeated), it falls back to the workflow's own StartTime — still a
// liveness signal (the run exists and started), just a coarser one.
func (h *TemporalHeartbeatSource) Heartbeat(ctx context.Context, workflowID string) (time.Time, error) {
	resp, err := h.Client.DescribeWorkflowExecution(ctx, workflowID, "")
	if err != nil {
		return time.Time{}, fmt.Errorf("recovery: describe workflow %s: %w", workflowID, err)
	}

	var freshest time.Time
	for _, pa := range resp.GetPendingActivities() {
		if ts := pa.GetLastHeartbeatTime(); ts != nil {
			if t := ts.AsTime(); t.After(freshest) {
				freshest = t
			}
		}
	}
	if !freshest.IsZero() {
		return freshest, nil
	}

	if info := resp.GetWorkflowExecutionInfo(); info != nil {
		if st := info.GetStartTime(); st != nil {
			return st.AsTime(), nil
		}
	}
	return time.Time{}, nil
}
