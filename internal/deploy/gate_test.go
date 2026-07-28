package deploy

import "testing"

func allPassInput() GateInput {
	return GateInput{
		PersonalProfile:                               true,
		DeploymentTargetAllowlisted:                   true,
		MissionReadinessComplete:                      true,
		SpendWithinEnvelope:                           true,
		DeterministicVerificationPassed:               true,
		SyntheticOrRealCanaryPassed:                   true,
		RollbackRehearsed:                             true,
		DatabaseChangesReversibleOrBackwardCompatible: true,
		NoRegulatedData:                               true,
		NoNewSecretScope:                              true,
		NoAuthorityExpansion:                          true,
		HealthChecksDefined:                           true,
		OperationReconciliationEnabled:                true,
	}
}

func TestGateMatrix_13Requirements_PassFail(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GateInput)
		fail   string
	}{
		{"personal-profile", func(in *GateInput) { in.PersonalProfile = false }, "personal-profile"},
		{"deployment-target-allowlisted", func(in *GateInput) { in.DeploymentTargetAllowlisted = false }, "deployment-target-allowlisted"},
		{"mission-readiness-complete", func(in *GateInput) { in.MissionReadinessComplete = false }, "mission-readiness-complete"},
		{"spend-within-envelope", func(in *GateInput) { in.SpendWithinEnvelope = false }, "spend-within-envelope"},
		{"deterministic-verification-passed", func(in *GateInput) { in.DeterministicVerificationPassed = false }, "deterministic-verification-passed"},
		{"synthetic-or-real-canary-passed", func(in *GateInput) { in.SyntheticOrRealCanaryPassed = false }, "synthetic-or-real-canary-passed"},
		{"rollback-rehearsed", func(in *GateInput) { in.RollbackRehearsed = false }, "rollback-rehearsed"},
		{"database-changes-reversible-or-backward-compatible", func(in *GateInput) { in.DatabaseChangesReversibleOrBackwardCompatible = false }, "database-changes-reversible-or-backward-compatible"},
		{"no-regulated-data", func(in *GateInput) { in.NoRegulatedData = false }, "no-regulated-data"},
		{"no-new-secret-scope", func(in *GateInput) { in.NoNewSecretScope = false }, "no-new-secret-scope"},
		{"no-authority-expansion", func(in *GateInput) { in.NoAuthorityExpansion = false }, "no-authority-expansion"},
		{"health-checks-defined", func(in *GateInput) { in.HealthChecksDefined = false }, "health-checks-defined"},
		{"operation-reconciliation-enabled", func(in *GateInput) { in.OperationReconciliationEnabled = false }, "operation-reconciliation-enabled"},
	}

	for _, tc := range tests {
		t.Run(tc.name+"-pass", func(t *testing.T) {
			got := EvaluateGate(allPassInput())
			if !got.Passed || got.Mode != "auto" {
				t.Fatalf("pass case failed: %+v", got)
			}
		})
		t.Run(tc.name+"-fail", func(t *testing.T) {
			in := allPassInput()
			tc.mutate(&in)
			got := EvaluateGate(in)
			if got.Passed || got.Mode != "command" || got.WaitReason != "human-approval" {
				t.Fatalf("fail case expected command/human-approval: %+v", got)
			}
			found := false
			for _, f := range got.Failed {
				if f == tc.fail {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("failed requirement %q not listed: %+v", tc.fail, got.Failed)
			}
		})
	}
}
