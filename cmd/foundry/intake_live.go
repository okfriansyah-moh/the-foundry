package main

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// startMissionViaTemporal is the live mission-start seam for Task 144:
// production intake starts MissionLoop through Temporal without a manual
// "start mission separately" handoff.
func startMissionViaTemporal(f *intakeFlags, in intake.StartMissionInput) (intake.StartMissionOutput, error) {
	host := f.temporalHostPort
	if host == "" {
		return intake.StartMissionOutput{}, fmt.Errorf("mission start --idea: TEMPORAL_HOSTPORT / --temporal-hostport required for live start")
	}
	c, err := client.Dial(client.Options{HostPort: host})
	if err != nil {
		return intake.StartMissionOutput{}, fmt.Errorf("mission start --idea: dial temporal: %w", err)
	}
	defer c.Close()

	missionID := "mission-" + in.RunID
	workflowID := "missionloop-" + missionID
	_, err = c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "foundry-core",
	}, mission.MissionLoop, mission.MissionLoopInput{
		MissionID: missionID,
	})
	if err != nil {
		return intake.StartMissionOutput{}, fmt.Errorf("mission start --idea: start MissionLoop: %w", err)
	}
	return intake.StartMissionOutput{MissionID: missionID}, nil
}
