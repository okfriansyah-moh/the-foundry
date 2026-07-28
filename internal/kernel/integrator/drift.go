package integrator

import (
	"fmt"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// RebasePolicy controls how the integrator handles drift.
type RebasePolicy string

const (
	// RebasePolicyCleanOnly attempts a clean rebase only (default for org workflows).
	// If the rebase has conflicts, the integrator immediately escalates to PROVEN_BLOCKED.
	RebasePolicyCleanOnly RebasePolicy = "rebase-clean-only"

	// RebasePolicyNone disables rebase; any drift → immediate PROVEN_BLOCKED.
	RebasePolicyNone RebasePolicy = "none"
)

// DriftGuardConfig is the policy knob for drift handling.
type DriftGuardConfig struct {
	// Policy determines rebase behavior on drift.
	Policy RebasePolicy
	// MaxRetries is the maximum number of requeue attempts before PROVEN_BLOCKED.
	MaxRetries int
}

// DefaultDriftGuardConfig returns the org-default policy (rebase-clean-only, 3 retries).
func DefaultDriftGuardConfig() DriftGuardConfig {
	return DriftGuardConfig{
		Policy:     RebasePolicyCleanOnly,
		MaxRetries: 3,
	}
}

// DriftResolution is the outcome of a drift guard evaluation.
type DriftResolution string

const (
	// DriftResolutionRequeued means the item was requeued for a clean rebase attempt.
	DriftResolutionRequeued DriftResolution = "requeued"
	// DriftResolutionBlocked means the item could not be resolved and is PROVEN_BLOCKED.
	DriftResolutionBlocked DriftResolution = "proven-blocked"
)

// DriftGuardResult is the result of evaluating a drifted item.
type DriftGuardResult struct {
	Resolution DriftResolution
	ResultCode state.ResultCode
	NextAction string
	RetryCount int
}

// ErrProvenBlocked is returned when drift cannot be resolved and the item
// must be marked FAILED/PROVEN_BLOCKED with a human next_action.
var ErrProvenBlocked = fmt.Errorf("integrator: %w", fmt.Errorf("drift cannot be resolved: %w", ErrDriftDetected))

// EvaluateDrift evaluates how to handle a drifted integration item given the
// current retry count and org policy. Returns the resolution and next action.
//
// rebaseClean simulates a clean rebase: true = no conflicts, false = conflicts.
// In production, this is determined by the actual rebase attempt on the worktree.
func EvaluateDrift(config DriftGuardConfig, item IntegrationItem, retryCount int, rebaseClean bool) DriftGuardResult {
	switch config.Policy {
	case RebasePolicyNone:
		return DriftGuardResult{
			Resolution: DriftResolutionBlocked,
			ResultCode: state.ResultProvenBlocked,
			NextAction: fmt.Sprintf("manual rebase of group %s onto new branch head", item.GroupID),
			RetryCount: retryCount,
		}
	case RebasePolicyCleanOnly:
		if !rebaseClean {
			return DriftGuardResult{
				Resolution: DriftResolutionBlocked,
				ResultCode: state.ResultProvenBlocked,
				NextAction: fmt.Sprintf("manual rebase of group %s onto new branch head (conflicting changes detected)", item.GroupID),
				RetryCount: retryCount,
			}
		}
		if retryCount >= config.MaxRetries {
			return DriftGuardResult{
				Resolution: DriftResolutionBlocked,
				ResultCode: state.ResultProvenBlocked,
				NextAction: fmt.Sprintf("manual rebase of group %s onto new branch head (max retries %d exceeded)", item.GroupID, config.MaxRetries),
				RetryCount: retryCount,
			}
		}
		return DriftGuardResult{
			Resolution: DriftResolutionRequeued,
			ResultCode: "",
			NextAction: fmt.Sprintf("requeued group %s for rebase attempt %d", item.GroupID, retryCount+1),
			RetryCount: retryCount + 1,
		}
	default:
		return DriftGuardResult{
			Resolution: DriftResolutionBlocked,
			ResultCode: state.ResultProvenBlocked,
			NextAction: fmt.Sprintf("unknown policy %q — treating as PROVEN_BLOCKED", config.Policy),
			RetryCount: retryCount,
		}
	}
}
