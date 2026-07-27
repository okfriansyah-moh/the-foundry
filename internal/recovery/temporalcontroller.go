// temporalcontroller.go: docs/PLAN.md Task 94 (FND-13R)'s live
// WorkflowController — see supervisor.go's own doc comment. Reset walks
// the target workflow's own history to find the most recent
// WORKFLOW_TASK_COMPLETED event and resets to it (disaster-recovery.md
// §20.10.3: "fence old worker -> load last checkpoint ... -> assign
// wake_at or live lease"): Temporal terminates the current run and starts
// a new one replaying history up to that point, which is exactly "load
// last checkpoint" for a DeadWorker/StuckActivity repair.
package recovery

import (
	"context"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// TemporalController implements WorkflowController against a real
// Temporal server.
type TemporalController struct {
	Client    client.Client
	Namespace string
}

// Reset implements WorkflowController.Reset.
func (c *TemporalController) Reset(ctx context.Context, workflowID string) error {
	iter := c.Client.GetWorkflowHistory(ctx, workflowID, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)

	var lastCompletedEventID int64
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			return fmt.Errorf("recovery: get workflow history for %s: %w", workflowID, err)
		}
		if event.GetEventType() == enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED {
			lastCompletedEventID = event.GetEventId()
		}
	}
	if lastCompletedEventID == 0 {
		return fmt.Errorf("recovery: no WORKFLOW_TASK_COMPLETED event found in history for workflow %s", workflowID)
	}

	_, err := c.Client.ResetWorkflowExecution(ctx, &workflowservice.ResetWorkflowExecutionRequest{
		Namespace: c.Namespace,
		WorkflowExecution: &commonpb.WorkflowExecution{
			WorkflowId: workflowID,
		},
		Reason:                    "recovery: liveness supervisor auto-repair",
		WorkflowTaskFinishEventId: lastCompletedEventID,
	})
	if err != nil {
		return fmt.Errorf("recovery: reset workflow %s: %w", workflowID, err)
	}
	return nil
}
