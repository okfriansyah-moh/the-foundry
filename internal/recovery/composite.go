// composite.go: docs/PLAN.md Task 94 (FND-13R)'s ProjectionSource that
// joins postgres.go's projection read with temporalheartbeat.go's live
// heartbeat read — the pairing supervisor.go's own doc comment names as
// this task's Outputs.
package recovery

import (
	"context"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// HeartbeatSource reads one RUNNING workflow's freshest liveness signal.
// TemporalHeartbeatSource is the only production implementation.
type HeartbeatSource interface {
	Heartbeat(ctx context.Context, workflowID string) (time.Time, error)
}

// CompositeProjectionSource is the real, live ProjectionSource Supervisor
// is wired against in cmd/foundryd/main.go: PG supplies every nonterminal
// workflow's projected state, and Heartbeats fills in LastHeartbeat for
// each RUNNING one (WAITING snapshots have no activity to heartbeat, so
// Classify never looks at LastHeartbeat for them).
type CompositeProjectionSource struct {
	PG         ProjectionSource
	Heartbeats HeartbeatSource
}

// ListNonterminal implements ProjectionSource.
func (c *CompositeProjectionSource) ListNonterminal(ctx context.Context) ([]WorkflowSnapshot, error) {
	snaps, err := c.PG.ListNonterminal(ctx)
	if err != nil {
		return nil, err
	}

	for i := range snaps {
		if snaps[i].Status != state.StatusRunning {
			continue
		}
		hb, err := c.Heartbeats.Heartbeat(ctx, snaps[i].WorkflowID)
		if err != nil {
			return nil, fmt.Errorf("recovery: heartbeat for %s: %w", snaps[i].WorkflowID, err)
		}
		snaps[i].LastHeartbeat = hb
	}
	return snaps, nil
}
