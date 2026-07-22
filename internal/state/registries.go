package state

// Phase registry (state-model.md §2, "Phase registry"). Extensible via
// governance, not ad hoc — this list must match the doc verbatim (see
// TestPhaseRegistryMatchesGoverningDoc).
const (
	PhaseIntake           Phase = "intake"
	PhaseContextGathering Phase = "context-gathering"
	PhaseSpecification    Phase = "specification"
	PhasePlanning         Phase = "planning"
	PhaseAdmission        Phase = "admission"
	PhaseImplementation   Phase = "implementation"
	PhaseVerifying        Phase = "verifying"
	PhaseReviewing        Phase = "reviewing"
	PhaseIntegrating      Phase = "integrating"
	PhaseDeploying        Phase = "deploying"
	PhaseObserving        Phase = "observing"
	PhaseImproving        Phase = "improving"
	PhaseCurating         Phase = "curating"
)

// phaseRegistry is the ordered, canonical phase list — the order matches
// state-model.md §2 verbatim and is used by the golden doc-diff test.
var phaseRegistry = []Phase{
	PhaseIntake,
	PhaseContextGathering,
	PhaseSpecification,
	PhasePlanning,
	PhaseAdmission,
	PhaseImplementation,
	PhaseVerifying,
	PhaseReviewing,
	PhaseIntegrating,
	PhaseDeploying,
	PhaseObserving,
	PhaseImproving,
	PhaseCurating,
}

// KnownPhase reports whether p is a member of the phase registry.
func KnownPhase(p Phase) bool {
	for _, known := range phaseRegistry {
		if known == p {
			return true
		}
	}
	return false
}

// Wait-reason registry (state-model.md §2, "Wait-reason registry"). Order
// matches the doc verbatim.
const (
	ReasonProviderCapacity    Reason = "provider-capacity"
	ReasonProviderOutage      Reason = "provider-outage"
	ReasonRateReset           Reason = "rate-reset"
	ReasonSubscriptionReset   Reason = "subscription-reset"
	ReasonHumanApproval       Reason = "human-approval"
	ReasonHumanCommand        Reason = "human-command"
	ReasonExternalDeployment  Reason = "external-deployment"
	ReasonBudget              Reason = "budget"
	ReasonSecurityHold        Reason = "security-hold"
	ReasonBlockedDependency   Reason = "blocked-dependency"
	ReasonUnforeseenHumanGate Reason = "unforeseen-human-gate"
	ReasonRetryBackoff        Reason = "retry-backoff"
)

// reasonRegistry is the ordered, canonical wait-reason list — the order
// matches state-model.md §2 verbatim and is used by the golden doc-diff test.
var reasonRegistry = []Reason{
	ReasonProviderCapacity,
	ReasonProviderOutage,
	ReasonRateReset,
	ReasonSubscriptionReset,
	ReasonHumanApproval,
	ReasonHumanCommand,
	ReasonExternalDeployment,
	ReasonBudget,
	ReasonSecurityHold,
	ReasonBlockedDependency,
	ReasonUnforeseenHumanGate,
	ReasonRetryBackoff,
}

// KnownReason reports whether r is a member of the wait-reason registry.
func KnownReason(r Reason) bool {
	for _, known := range reasonRegistry {
		if known == r {
			return true
		}
	}
	return false
}

// Terminal result-code registry (state-model.md §2, "Terminal result-code
// registry (initial)"). Order and the on-status mapping match the doc's
// fenced code block verbatim.
const (
	ResultProvenBlocked             ResultCode = "PROVEN_BLOCKED"
	ResultAdmissionRejected         ResultCode = "ADMISSION_REJECTED"
	ResultRolledBack                ResultCode = "ROLLED_BACK"
	ResultTenXBranchHandoffReady    ResultCode = "TEN_X_BRANCH_HANDOFF_READY"
	ResultMissionTargetReached      ResultCode = "MISSION_TARGET_REACHED"
	ResultMissionNoViableCandidate  ResultCode = "MISSION_NO_VIABLE_CANDIDATE"
	ResultMissionBudgetExhausted    ResultCode = "MISSION_BUDGET_EXHAUSTED"
	ResultMissionTerminatedByPolicy ResultCode = "MISSION_TERMINATED_BY_POLICY"
	ResultMissionKilled             ResultCode = "MISSION_KILLED"
	ResultMissionMaintenanceMode    ResultCode = "MISSION_MAINTENANCE_MODE"
)

// resultCodeEntry pairs a registered result code with the single Status it is
// valid on, preserving the doc's declaration order for the golden diff test.
type resultCodeEntry struct {
	Code   ResultCode
	Status Status
}

// resultCodeRegistry is the ordered, canonical terminal result-code list.
var resultCodeRegistry = []resultCodeEntry{
	{ResultProvenBlocked, StatusFailed},
	{ResultAdmissionRejected, StatusFailed},
	{ResultRolledBack, StatusFailed},
	{ResultTenXBranchHandoffReady, StatusSucceeded},
	{ResultMissionTargetReached, StatusSucceeded},
	{ResultMissionNoViableCandidate, StatusFailed},
	{ResultMissionBudgetExhausted, StatusFailed},
	{ResultMissionTerminatedByPolicy, StatusCancelled},
	{ResultMissionKilled, StatusCancelled},
	{ResultMissionMaintenanceMode, StatusSucceeded},
}

// KnownResultCode reports whether c is a member of the terminal result-code
// registry, and if so, the Status it is valid on.
func KnownResultCode(c ResultCode) (Status, bool) {
	for _, entry := range resultCodeRegistry {
		if entry.Code == c {
			return entry.Status, true
		}
	}
	return "", false
}
