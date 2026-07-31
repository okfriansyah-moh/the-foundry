package notify

import (
	"fmt"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

type DigestV2 struct {
	Promotions  []DigestPromotion
	Window      evolve.BudgetWindow
	Limits      evolve.ChangeBudgetLimits
	Placeholder bool
	// Cost, when set, renders the reserved/incurred/reconciled/shadow figures
	// for the reporting window (docs/PLAN.md Task 120 / COST-02): subscription
	// shadow spend is visible in the digest, not invisible.
	Cost *DigestCost
}

// DigestCost is the four-figure cost panel for the digest.
type DigestCost struct {
	ReservedUSD   float64
	IncurredUSD   float64
	ReconciledUSD float64
	ShadowUSD     float64
	// ShadowCeilingUSD is the subscription-period shadow ceiling; when >0 the
	// digest shows how much of it is used and flags a breach.
	ShadowCeilingUSD float64
}

func FormatDigestV2(d DigestV2) string {
	var sb strings.Builder
	sb.WriteString("📈 *Weekly Governance Digest v2*\n")
	if d.Placeholder {
		sb.WriteString("placeholder: true — conservative numbers in effect\n")
	}
	sb.WriteString(fmt.Sprintf("promotions %s\n", bar(d.Window.Promotions, d.Limits.MaxPromotions)))
	sb.WriteString(fmt.Sprintf("files changed %s\n", bar(d.Window.FilesChanged, d.Limits.MaxFilesChanged)))
	sb.WriteString(fmt.Sprintf("rollback depth %s\n", bar(d.Window.RollbackChainDepth, d.Limits.MaxRollbackDepth)))
	if d.Cost != nil {
		c := d.Cost
		sb.WriteString(fmt.Sprintf("cost: reserved $%.2f · incurred $%.2f · reconciled $%.2f · shadow $%.2f\n",
			c.ReservedUSD, c.IncurredUSD, c.ReconciledUSD, c.ShadowUSD))
		if c.ShadowCeilingUSD > 0 {
			breached := ""
			if c.ShadowUSD >= c.ShadowCeilingUSD {
				breached = " ⚠️ CEILING BREACHED"
			}
			sb.WriteString(fmt.Sprintf("shadow ceiling: $%.2f of $%.2f%s\n", c.ShadowUSD, c.ShadowCeilingUSD, breached))
		}
	}
	for _, promotion := range d.Promotions {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", promotion.ProductID, shortRef(promotion.ChangeRef)))
	}
	if evolve.IsFrozen() {
		sb.WriteString(fmt.Sprintf("FROZEN: %s\n", evolve.FreezeReason()))
	}
	return sb.String()
}

func bar(value, limit int) string {
	if limit <= 0 {
		return fmt.Sprintf("%d/∞", value)
	}
	filled := value * 10 / limit
	if filled > 10 {
		filled = 10
	}
	return fmt.Sprintf("[%s%s] %d/%d", strings.Repeat("#", filled), strings.Repeat("-", 10-filled), value, limit)
}
