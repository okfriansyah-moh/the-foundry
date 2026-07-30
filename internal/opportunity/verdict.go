package opportunity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Named reject reasons (Phase A's explicit rejection rules) and named unmet
// threshold identifiers (Phase D). Returning stable names lets the store and
// the digest bind a verdict to exactly why it was reached.
const (
	RejectNoEvidence          = "reject:no-evidence"
	RejectNoReachableCustomer = "reject:no-reachable-customer"
	RejectWeakGrossMargin     = "reject:weak-gross-margin"
	RejectPlatformPolicyRisk  = "reject:platform-policy-risk"
	RejectValuePropOnlyAI     = "reject:value-prop-only-ai"
	RejectAllAssumed          = "reject:all-assumed-or-unresolved"

	UnmetTotalScore        = "unmet:minimum_total_score"
	UnmetDistributionScore = "unmet:minimum_distribution_score"
	UnmetPaymentEvidence   = "unmet:minimum_payment_evidence_score"
	UnmetRealValidation    = "unmet:must_have_real_validation_signal"
	UnmetMVPBudget         = "unmet:maximum_mvp_budget_usd"
)

// Decide returns the verdict and the ordered list of the reasons a BUILD was
// not reached (named reject reasons first, then unmet threshold names).
//
// Contract (docs/PLAN.md Task 100 Step 4):
//   - REJECT is the default whenever any required threshold is unevaluable —
//     never BUILD.
//   - An opportunity whose evidence never exceeds Assumed can never BUILD,
//     regardless of total score.
//   - Each of the Phase-D numeric thresholds is independently sufficient to
//     block BUILD.
func Decide(sc Scorecard, t Thresholds) (Verdict, []string) {
	var blockers []string

	// Named Phase-A reject rules (hard rejects).
	sig := sc.Signals
	if !sig.HasAnyEvidence {
		blockers = append(blockers, RejectNoEvidence)
	}
	if !sig.HasReachableChannel || sig.DistributionScore <= 0 {
		blockers = append(blockers, RejectNoReachableCustomer)
	}
	if sig.WeakGrossMargin {
		blockers = append(blockers, RejectWeakGrossMargin)
	}
	if sig.PlatformPolicyRisk {
		blockers = append(blockers, RejectPlatformPolicyRisk)
	}
	if sig.ValuePropOnlyAI {
		blockers = append(blockers, RejectValuePropOnlyAI)
	}
	hardReject := len(blockers) > 0

	// Evidence-strength guard: never BUILD when the best evidence is Assumed
	// or weaker.
	evidenceTooWeak := sig.MaxLabelStrength < sig.StrongEvidenceFloor
	if evidenceTooWeak {
		blockers = append(blockers, RejectAllAssumed)
	}

	// Phase-D numeric thresholds — each independently sufficient to block BUILD.
	var unmet []string
	if sc.Total < t.MinimumTotalScore {
		unmet = append(unmet, UnmetTotalScore)
	}
	if sig.DistributionScore < t.MinimumDistributionScore {
		unmet = append(unmet, UnmetDistributionScore)
	}
	if sig.PaymentEvidenceScore < t.MinimumPaymentEvidenceScore {
		unmet = append(unmet, UnmetPaymentEvidence)
	}
	if t.MustHaveRealValidationSignal && !sig.RealValidationSignal {
		unmet = append(unmet, UnmetRealValidation)
	}
	if t.MaximumMVPBudgetUSD > 0 && sig.MVPBudgetUSD > t.MaximumMVPBudgetUSD {
		unmet = append(unmet, UnmetMVPBudget)
	}
	blockers = append(blockers, unmet...)

	// BUILD requires: no hard reject, evidence strong enough, and no unmet
	// numeric threshold.
	if !hardReject && !evidenceTooWeak && len(unmet) == 0 {
		return VerdictBuild, nil
	}

	// A hard reject is terminal. Otherwise, an opportunity that clears the
	// VALIDATE-MORE floor gets one more bounded experiment; below it, REJECT.
	if hardReject {
		return VerdictReject, blockers
	}
	if sc.Total >= t.ValidateMoreMinScore {
		return VerdictValidateMore, blockers
	}
	return VerdictReject, blockers
}

// ThresholdsDigest returns the hex sha256 of a byte-stable encoding of the
// thresholds, so a verdict records exactly which gates were in force.
func ThresholdsDigest(t Thresholds) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		return "", fmt.Errorf("opportunity: canonicalize thresholds: %w", err)
	}
	sum := sha256.Sum256(bytes.TrimRight(buf.Bytes(), "\n"))
	return hex.EncodeToString(sum[:]), nil
}
