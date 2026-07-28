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
