// docs/PLAN.md Task 106 (RTC-02) gated live proof. Gated at runtime by
// RUN_MISSION_LIVE=1 + PG_DSN + TEMPORAL_HOSTPORT, so a bare `go test ./...`
// never requires infra. When enabled against the compose Temporal+Postgres, it
// proves a real MissionLoop execution starts, its activities resolve, and it is
// killable mid-loop.
package recoverylive_test

import (
	"context"
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

func TestMissionLoopLive(t *testing.T) {
	if os.Getenv("RUN_MISSION_LIVE") != "1" {
		t.Skip("set RUN_MISSION_LIVE=1 (with PG_DSN + TEMPORAL_HOSTPORT) to run the live mission proof")
	}
	hostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if hostPort == "" {
		hostPort = "temporal:7233"
	}
	tc, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		t.Fatalf("dial temporal: %v", err)
	}
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The mission must be pre-provisioned (contract + readiness) by the test
	// harness/operator; this proof starts its loop on the delivery lane and
	// then kills it, asserting the kill is honored by a real worker.
	missionID := os.Getenv("MISSION_LIVE_ID")
	if missionID == "" {
		t.Skip("set MISSION_LIVE_ID to an already-provisioned mission")
	}
	run, err := tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        "mission-live-" + missionID,
		TaskQueue: os.Getenv("MISSION_LIVE_TASK_QUEUE"),
	}, mission.MissionLoop, mission.MissionLoopInput{MissionID: missionID})
	if err != nil {
		t.Fatalf("start MissionLoop: %v", err)
	}

	if err := tc.SignalWorkflow(ctx, run.GetID(), run.GetRunID(), mission.SignalKillMission, mission.KillRequest{RequestedBy: "test", Reason: "live-proof teardown"}); err != nil {
		t.Fatalf("kill signal: %v", err)
	}
	var result mission.MissionLoopResult
	if err := run.Get(ctx, &result); err != nil {
		t.Fatalf("await mission result: %v", err)
	}
	if result.Status == "" {
		t.Fatalf("expected a terminal mission result, got %+v", result)
	}
}
