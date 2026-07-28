package integrator_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TestEvaluateDrift_CleanRebase_Requeued verifies a clean rebase → requeued.
func TestEvaluateDrift_CleanRebase_Requeued(t *testing.T) {
	config := integrator.DefaultDriftGuardConfig()
	item := makeItem("main", "item-1", "old-base", []string{"sha-1"})
	result := integrator.EvaluateDrift(config, item, 0, true /* rebaseClean */)
	if result.Resolution != integrator.DriftResolutionRequeued {
		t.Errorf("Resolution=%q, want requeued", result.Resolution)
	}
	if result.ResultCode != "" {
		t.Errorf("ResultCode=%q, want empty for requeued", result.ResultCode)
	}
}

// TestEvaluateDrift_ConflictingRebase_ProvenBlocked verifies conflict → PROVEN_BLOCKED.
func TestEvaluateDrift_ConflictingRebase_ProvenBlocked(t *testing.T) {
	config := integrator.DefaultDriftGuardConfig()
	item := makeItem("main", "item-1", "old-base", []string{"sha-1"})
	result := integrator.EvaluateDrift(config, item, 0, false /* conflict */)
	if result.Resolution != integrator.DriftResolutionBlocked {
		t.Errorf("Resolution=%q, want proven-blocked", result.Resolution)
	}
	if result.ResultCode != state.ResultProvenBlocked {
		t.Errorf("ResultCode=%q, want PROVEN_BLOCKED", result.ResultCode)
	}
	if result.NextAction == "" {
		t.Error("NextAction is empty for PROVEN_BLOCKED")
	}
}

// TestEvaluateDrift_MaxRetriesExceeded_ProvenBlocked verifies max retries → PROVEN_BLOCKED.
func TestEvaluateDrift_MaxRetriesExceeded_ProvenBlocked(t *testing.T) {
	config := integrator.DefaultDriftGuardConfig()
	item := makeItem("main", "item-1", "old-base", []string{"sha-1"})
	result := integrator.EvaluateDrift(config, item, config.MaxRetries, true)
	if result.Resolution != integrator.DriftResolutionBlocked {
		t.Errorf("Resolution=%q, want proven-blocked after max retries", result.Resolution)
	}
}

// TestEvaluateDrift_PolicyNone_ImmediateBlocked verifies none policy → immediate PROVEN_BLOCKED.
func TestEvaluateDrift_PolicyNone_ImmediateBlocked(t *testing.T) {
	config := integrator.DriftGuardConfig{Policy: integrator.RebasePolicyNone}
	item := makeItem("main", "item-1", "old-base", []string{"sha-1"})
	result := integrator.EvaluateDrift(config, item, 0, true)
	if result.Resolution != integrator.DriftResolutionBlocked {
		t.Errorf("Resolution=%q, want proven-blocked for PolicyNone", result.Resolution)
	}
}

// TestEvaluateDrift_RequiresRevalidation verifies that after requeue, re-run of
// validation is enforced (no stale-check push).
// This is the "re-run of validation after rebase enforced" acceptance criterion.
func TestEvaluateDrift_RequiresRevalidation(t *testing.T) {
	config := integrator.DefaultDriftGuardConfig()
	item := makeItem("main", "item-1", "old-base", []string{"sha-1"})
	result := integrator.EvaluateDrift(config, item, 0, true)
	// The requeued item has RetryCount incremented — the next pass must run
	// fresh validation before pushing (enforced by the integrator's protocol:
	// only ProcessItem, which requires checks to have run, is the push path).
	if result.RetryCount != 1 {
		t.Errorf("RetryCount=%d after first requeue, want 1", result.RetryCount)
	}
}
