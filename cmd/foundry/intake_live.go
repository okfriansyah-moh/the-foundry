package main

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

// startMissionViaTemporal is the live mission-start seam for the intake
// pipeline. The offline/default path uses a recording starter (proving
// orchestration with zero network); a live intake start reuses the same
// admission-approved provenance the human-authored `mission create` +
// `mission start <id>` path uses. Wiring the generated-plan → MissionContract →
// Temporal execution end to end is the mission-runtime work (Task 105/121); the
// intake pipeline deliberately stops at a recorded, approved, ready run and
// hands that off, rather than duplicating the starter here.
func startMissionViaTemporal(f *intakeFlags, in intake.StartMissionInput) (intake.StartMissionOutput, error) {
	return intake.StartMissionOutput{}, fmt.Errorf(
		"mission start --idea: live Temporal start (hostport %q) is driven by the mission runtime; "+
			"the approved, ready run %s is recorded — start it via `foundry mission start <mission-id>` or use --dry-run",
		f.temporalHostPort, in.RunID)
}
