package bench

import (
	"fmt"
	"sort"
)

// Unit names the physical unit for a metric value.
type Unit string

const (
	UnitHours    Unit = "hours"
	UnitCount    Unit = "count"
	UnitRatio    Unit = "ratio"
	UnitUSD      Unit = "usd"
	UnitUSDPerTask Unit = "usd_per_task"
)

// Arm identifies which experimental arm produced a run record.
type Arm string

const (
	ArmControl Arm = "control"
	ArmFoundry Arm = "foundry"
)

// Basis states how a metric value was obtained.
type Basis string

const (
	BasisInstrumented    Basis = "instrumented"
	BasisHumanReported   Basis = "human-reported"
	BasisNotMeasurable   Basis = "not-measurable"
	BasisProxy           Basis = "proxy"
)

// MetricID is the stable identifier for a V1 acceleration metric.
type MetricID string

const (
	MetricRequirementToPlan       MetricID = "requirement_to_plan"
	MetricPlanToFirstAccepted     MetricID = "plan_to_first_accepted"
	MetricPlanToVerified          MetricID = "plan_to_verified_completion"
	MetricPlanToTenXHandoff       MetricID = "plan_to_tenx_handoff"
	MetricHumanOrchestration      MetricID = "human_orchestration_time"
	MetricManualPrompts           MetricID = "manual_prompts_touches"
	MetricUnattendedRuntime       MetricID = "unattended_runtime"
	MetricRecoveryTime            MetricID = "recovery_time"
	MetricRetryRate               MetricID = "retry_rate"
	MetricEvidenceRejectionRate   MetricID = "evidence_rejection_rate"
	MetricIntegrationConflicts    MetricID = "integration_conflicts"
	MetricDefectsAfterHandoff     MetricID = "defects_after_handoff"
	MetricUnauthorizedActions     MetricID = "unauthorized_actions"
	MetricAIProviderCost          MetricID = "ai_provider_cost"
	MetricCostPerAcceptedTask     MetricID = "cost_per_accepted_task"
)

// Definition describes one metric's unit, observation point, and when it
// cannot be measured in a given arm.
type Definition struct {
	ID               MetricID
	Label            string
	Unit             Unit
	ObservationPoint string
	NotMeasurableRule string
}

// AllMetrics returns every V1 acceleration metric in stable ID order.
func AllMetrics() []Definition {
	out := []Definition{
		{
			ID:               MetricRequirementToPlan,
			Label:            "Requirement → executable PLAN",
			Unit:             UnitHours,
			ObservationPoint: "Time from requirement/idea capture to an executable PLAN artifact existing in the repo.",
			NotMeasurableRule: "Control arm: no requirement timestamp in git — record not-measurable. Foundry arm: intake pipeline start → PLAN commit (Task 135).",
		},
		{
			ID:               MetricPlanToFirstAccepted,
			Label:            "PLAN → first accepted change",
			Unit:             UnitHours,
			ObservationPoint: "Time from PLAN availability to the first accepted SCM change (merge or approval).",
			NotMeasurableRule: "Control arm: PLAN event absent — use git proxy first-branch-commit → merge, flagged proxy. Foundry arm: instrumented from kernel transitions (Task 135).",
		},
		{
			ID:               MetricPlanToVerified,
			Label:            "PLAN → verified completion",
			Unit:             UnitHours,
			ObservationPoint: "Time from PLAN availability to verified completion (evidence accepted).",
			NotMeasurableRule: "Control arm: no verification event — use git proxy first-branch-commit → merge, flagged proxy. Foundry arm: instrumented from evidence bundle acceptance (Task 135).",
		},
		{
			ID:               MetricPlanToTenXHandoff,
			Label:            "PLAN → 10x branch handoff",
			Unit:             UnitHours,
			ObservationPoint: "Time from PLAN approval to disposable remote branch handoff (10x path).",
			NotMeasurableRule: "Personal/control deliveries: not applicable — record not-measurable. 10x Foundry arm: instrumented from integrator queue (Task 135).",
		},
		{
			ID:               MetricHumanOrchestration,
			Label:            "Human orchestration time",
			Unit:             UnitHours,
			ObservationPoint: "Wall-clock hours the human spent directing, reviewing, and unblocking the delivery.",
			NotMeasurableRule: "Control arm: human-reported only (B12). Foundry arm: instrumented from human-touch ledger where available; otherwise human-reported.",
		},
		{
			ID:               MetricManualPrompts,
			Label:            "Manual prompts / touches",
			Unit:             UnitCount,
			ObservationPoint: "Count of manual prompts, approvals, or operator touches required to complete the delivery.",
			NotMeasurableRule: "Control arm: human-reported only (B12). Foundry arm: instrumented from human-touch ledger where available; otherwise human-reported.",
		},
		{
			ID:               MetricUnattendedRuntime,
			Label:            "Unattended runtime",
			Unit:             UnitHours,
			ObservationPoint: "Wall-clock time the delivery ran without human intervention.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: kernel workflow idle-wait minus human-touch windows (Task 135).",
		},
		{
			ID:               MetricRecoveryTime,
			Label:            "Recovery time",
			Unit:             UnitHours,
			ObservationPoint: "Time from failure detection to resumed forward progress.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: recovery supervisor escalations (Task 135).",
		},
		{
			ID:               MetricRetryRate,
			Label:            "Retry rate",
			Unit:             UnitRatio,
			ObservationPoint: "Fraction of task attempts that required at least one retry.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: Prometheus foundry_task_retries_total / attempts (Task 135).",
		},
		{
			ID:               MetricEvidenceRejectionRate,
			Label:            "Evidence rejection rate",
			Unit:             UnitRatio,
			ObservationPoint: "Fraction of evidence submissions rejected before acceptance.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: evidence bundle verify failures / submissions (Task 135).",
		},
		{
			ID:               MetricIntegrationConflicts,
			Label:            "Integration conflicts",
			Unit:             UnitCount,
			ObservationPoint: "Count of merge/integration conflicts requiring human resolution.",
			NotMeasurableRule: "Control arm: not logged — record not-measurable. Foundry arm: integrator conflict counter (Task 135).",
		},
		{
			ID:               MetricDefectsAfterHandoff,
			Label:            "Defects after handoff",
			Unit:             UnitCount,
			ObservationPoint: "Count of defects discovered after the delivery was handed off.",
			NotMeasurableRule: "Control arm: git-derived fix commits touching same files are proxy unless linked issue/incident confirms defect. Foundry arm: instrumented defect tracker + git corroboration (Task 135).",
		},
		{
			ID:               MetricUnauthorizedActions,
			Label:            "Unauthorized actions",
			Unit:             UnitCount,
			ObservationPoint: "Count of side effects attempted or executed without authorization.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: policy denial + audit ledger (must be 0 for V1 pass).",
		},
		{
			ID:               MetricAIProviderCost,
			Label:            "AI / provider cost",
			Unit:             UnitUSD,
			ObservationPoint: "Total provider cost for the delivery.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: cost ledger actuals (Task 135).",
		},
		{
			ID:               MetricCostPerAcceptedTask,
			Label:            "Cost per accepted task",
			Unit:             UnitUSDPerTask,
			ObservationPoint: "Total provider cost divided by count of accepted task completions.",
			NotMeasurableRule: "Control arm: not instrumented — record not-measurable. Foundry arm: cost ledger / accepted task count (Task 135).",
		},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DefinitionByID returns the metric definition for id.
func DefinitionByID(id MetricID) (Definition, error) {
	for _, d := range AllMetrics() {
		if d.ID == id {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("bench: unknown metric %q", id)
}

// QualityGuardMetrics are evaluated for the "quality no worse than baseline" gate.
func QualityGuardMetrics() []MetricID {
	return []MetricID{
		MetricDefectsAfterHandoff,
		MetricEvidenceRejectionRate,
	}
}
