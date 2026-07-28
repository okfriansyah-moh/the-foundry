package deploy

import "sort"

type GateInput struct {
	PersonalProfile                               bool
	DeploymentTargetAllowlisted                   bool
	MissionReadinessComplete                      bool
	SpendWithinEnvelope                           bool
	DeterministicVerificationPassed               bool
	SyntheticOrRealCanaryPassed                   bool
	RollbackRehearsed                             bool
	DatabaseChangesReversibleOrBackwardCompatible bool
	NoRegulatedData                               bool
	NoNewSecretScope                              bool
	NoAuthorityExpansion                          bool
	HealthChecksDefined                           bool
	OperationReconciliationEnabled                bool
}

type GateResult struct {
	Passed     bool
	Failed     []string
	Mode       string
	WaitReason string
}

func EvaluateGate(in GateInput) GateResult {
	failed := make([]string, 0, 13)
	if !in.PersonalProfile {
		failed = append(failed, "personal-profile")
	}
	if !in.DeploymentTargetAllowlisted {
		failed = append(failed, "deployment-target-allowlisted")
	}
	if !in.MissionReadinessComplete {
		failed = append(failed, "mission-readiness-complete")
	}
	if !in.SpendWithinEnvelope {
		failed = append(failed, "spend-within-envelope")
	}
	if !in.DeterministicVerificationPassed {
		failed = append(failed, "deterministic-verification-passed")
	}
	if !in.SyntheticOrRealCanaryPassed {
		failed = append(failed, "synthetic-or-real-canary-passed")
	}
	if !in.RollbackRehearsed {
		failed = append(failed, "rollback-rehearsed")
	}
	if !in.DatabaseChangesReversibleOrBackwardCompatible {
		failed = append(failed, "database-changes-reversible-or-backward-compatible")
	}
	if !in.NoRegulatedData {
		failed = append(failed, "no-regulated-data")
	}
	if !in.NoNewSecretScope {
		failed = append(failed, "no-new-secret-scope")
	}
	if !in.NoAuthorityExpansion {
		failed = append(failed, "no-authority-expansion")
	}
	if !in.HealthChecksDefined {
		failed = append(failed, "health-checks-defined")
	}
	if !in.OperationReconciliationEnabled {
		failed = append(failed, "operation-reconciliation-enabled")
	}
	sort.Strings(failed)
	if len(failed) > 0 {
		return GateResult{
			Passed:     false,
			Failed:     failed,
			Mode:       "command",
			WaitReason: "human-approval",
		}
	}
	return GateResult{Passed: true, Failed: nil, Mode: "auto"}
}
