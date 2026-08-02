package kernel

import (
	"fmt"
	"strings"
)

// docs/PLAN.md Task 148 (TX-13): ApprovedPlan-driven TenX orchestration input.

// TenXOrchestrationInput is the production start contract. Caller-supplied
// change sets are rejected; only immutable ApprovedPlan + envelope refs are
// authoritative.
type TenXOrchestrationInput struct {
	ApprovedPlanID string
	PlanDigest     string
	EnvelopeDigest string
	OrganizationID string
	ProfileID      string
	IdempotencyKey string
	// UntrustedChangeSets is accepted only to refuse: production must not
	// treat caller change sets as authoritative.
	UntrustedChangeSets []string
}

// ValidateTenXOrchestrationInput enforces Task 148 production start rules.
func ValidateTenXOrchestrationInput(in TenXOrchestrationInput) error {
	if strings.TrimSpace(in.ApprovedPlanID) == "" {
		return fmt.Errorf("kernel: tenx orchestration requires ApprovedPlanID")
	}
	if strings.TrimSpace(in.EnvelopeDigest) == "" {
		return fmt.Errorf("kernel: tenx orchestration requires EnvelopeDigest")
	}
	if strings.TrimSpace(in.PlanDigest) == "" {
		return fmt.Errorf("kernel: tenx orchestration requires PlanDigest")
	}
	if len(in.UntrustedChangeSets) > 0 {
		return fmt.Errorf("kernel: tenx orchestration rejects caller-supplied change sets (Task 148)")
	}
	return nil
}

// TenXOrchestrationPlan is the derived orchestration plan after PEC proposes
// and the kernel validates waves (PEC proposes only — C5).
type TenXOrchestrationPlan struct {
	RunID          string
	WaveDigest     string
	AtomicGroupIDs []string
	ManifestDigest string
	TaskIDs        []string
}

// DeriveTenXOrchestrationPlan builds a deterministic orchestration plan from
// verified task IDs (evidence-derived). Empty taskIDs refuse.
func DeriveTenXOrchestrationPlan(runID string, verifiedTaskIDs []string, waveDigest, manifestDigest string) (TenXOrchestrationPlan, error) {
	if runID == "" {
		return TenXOrchestrationPlan{}, fmt.Errorf("kernel: tenx orchestration run id required")
	}
	if len(verifiedTaskIDs) == 0 {
		return TenXOrchestrationPlan{}, fmt.Errorf("kernel: tenx orchestration requires verified tasks")
	}
	if waveDigest == "" || manifestDigest == "" {
		return TenXOrchestrationPlan{}, fmt.Errorf("kernel: tenx orchestration requires wave and manifest digests")
	}
	groups := make([]string, 0, len(verifiedTaskIDs))
	for _, id := range verifiedTaskIDs {
		groups = append(groups, "ag-"+id)
	}
	return TenXOrchestrationPlan{
		RunID:          runID,
		WaveDigest:     waveDigest,
		AtomicGroupIDs: groups,
		ManifestDigest: manifestDigest,
		TaskIDs:        append([]string(nil), verifiedTaskIDs...),
	}, nil
}
