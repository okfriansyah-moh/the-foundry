// Package notify — digest.go
//
// Task 52 (VEN-13): Weekly veto digest v0 (C11/C20 precursor).
//
// WeeklyDigest is the non-blocking governance channel for autonomous
// improvement cycles. Once per week (or on demand), it:
//
//  1. Collects promotions within the veto window (default 7 days).
//  2. Formats a Telegram digest per the governance spec:
//     — change list, before/after metrics, budget consumption placeholder,
//     — /rollback <promo-id> nonce-keyed veto link.
//  3. Records a 24h veto-window record per promotion.
//
// Veto execution: when an operator issues /rollback <promo-id>, the
// VetoExecutor rolls back to the promotion's rollback_ref, marks the row
// vetoed, and records a learning-evidence row.
//
// Freeze logic: rollback-chain depth >2 or vetoed-twice-same-target →
// ImprovementLeaseFrozen until `foundry promotions unfreeze` (audited).
package notify

import (
	"fmt"
	"strings"
	"time"
)

// DigestPromotion is a single promotion entry in the weekly digest.
type DigestPromotion struct {
	ID            string
	ProductID     string
	ChangeRef     string
	PlanDigest    string
	MetricsBefore map[string]float64
	MetricsAfter  map[string]float64
	RollbackRef   string
	CreatedAt     time.Time
}

// VetoRecord tracks the 24h veto window for one promotion.
type VetoRecord struct {
	PromotionID string
	ExpiresAt   time.Time
	Vetoed      bool
	VetoedAt    *time.Time
}

// FreezeReason explains why the improvement lease is frozen.
type FreezeReason string

const (
	FreezeReasonRollbackChain FreezeReason = "rollback-chain-depth-exceeded"
	FreezeReasonRepeatedVeto  FreezeReason = "vetoed-twice-same-target"
)

// VetoWindow is the fixed 24-hour window an operator has to veto a promotion.
const VetoWindow = 24 * time.Hour

// FormatWeeklyDigest renders the weekly digest message for a set of promotions.
// The format carries change list, before/after metrics, and /rollback nonce links.
func FormatWeeklyDigest(promotions []DigestPromotion, nonces map[string]string, now time.Time) string {
	if len(promotions) == 0 {
		return "📊 Weekly Improvement Digest\n\nNo promotions this week."
	}

	var sb strings.Builder
	sb.WriteString("📊 *Weekly Improvement Digest*\n")
	sb.WriteString(fmt.Sprintf("Period: %s\n\n", now.Format("2006-01-02")))

	for i, p := range promotions {
		sb.WriteString(fmt.Sprintf("*%d. Product: %s*\n", i+1, p.ProductID))
		sb.WriteString(fmt.Sprintf("   Change: `%s`\n", shortRef(p.ChangeRef)))
		sb.WriteString(fmt.Sprintf("   Plan digest: `%s`\n", shortDigest(p.PlanDigest)))
		if p.MetricsBefore != nil && p.MetricsAfter != nil {
			sb.WriteString(fmt.Sprintf("   MRR: $%.2f → $%.2f\n",
				p.MetricsBefore["net_mrr_usd"], p.MetricsAfter["net_mrr_usd"]))
			sb.WriteString(fmt.Sprintf("   Activation: %.1f%% → %.1f%%\n",
				p.MetricsBefore["activation_rate"]*100, p.MetricsAfter["activation_rate"]*100))
		}
		if nonce, ok := nonces[p.ID]; ok {
			sb.WriteString(fmt.Sprintf("   ⚠️  Veto within 24h: /rollback %s %s\n", p.ID, nonce))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("_No action = auto-continue. Veto window: 24h._")
	return sb.String()
}

// BuildVetoRecords creates VetoRecord entries for a set of promotions,
// with a 24h expiry from the digest time.
func BuildVetoRecords(promotions []DigestPromotion, now time.Time) []VetoRecord {
	records := make([]VetoRecord, len(promotions))
	for i, p := range promotions {
		records[i] = VetoRecord{
			PromotionID: p.ID,
			ExpiresAt:   now.Add(VetoWindow),
		}
	}
	return records
}

// IsVetoExpired reports whether a veto record's window has closed.
func IsVetoExpired(rec VetoRecord, now time.Time) bool {
	return now.After(rec.ExpiresAt)
}

// FreezeCheck inspects the veto/rollback history for a product and returns
// the FreezeReason if the improvement lease should be frozen, or "" if clear.
// Constitution C11/C20 basis: rollback-chain depth >2 or vetoed-twice-same-target.
func FreezeCheck(productID string, history []VetoRecord, rollbackChainDepth int) FreezeReason {
	if rollbackChainDepth > 2 {
		return FreezeReasonRollbackChain
	}
	vetoCount := 0
	for _, r := range history {
		if r.Vetoed {
			vetoCount++
		}
	}
	if vetoCount >= 2 {
		return FreezeReasonRepeatedVeto
	}
	return ""
}

// shortRef returns the first 8 characters of a ref for display.
func shortRef(ref string) string {
	if len(ref) > 8 {
		return ref[:8]
	}
	return ref
}

// shortDigest returns the first 12 characters of a digest for display.
func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}
