package notify

import (
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

func TestFormatDigestV2_PlaceholderBanner(t *testing.T) {
	d := DigestV2{
		Window:      evolve.BudgetWindow{Promotions: 2, FilesChanged: 5},
		Limits:      evolve.ChangeBudgetLimits{MaxPromotions: 5, MaxFilesChanged: 50},
		Placeholder: true,
	}
	out := FormatDigestV2(d)
	if !strings.Contains(out, "placeholder: true") {
		t.Fatalf("expected placeholder banner in digest v2, got: %s", out)
	}
	if !strings.Contains(out, "promotions") {
		t.Fatalf("expected budget bars in digest v2, got: %s", out)
	}
}

func TestFormatDigestV2_FrozenState(t *testing.T) {
	evolve.Freeze(evolve.FreezeCostSpike)
	defer evolve.Unfreeze()

	d := DigestV2{
		Window:      evolve.BudgetWindow{CostDeltaUSD: 200},
		Limits:      evolve.ChangeBudgetLimits{MaxCostDeltaUSD: 100},
		Placeholder: false,
	}
	out := FormatDigestV2(d)
	if !strings.Contains(out, "FROZEN") {
		t.Fatalf("expected FROZEN state in digest v2 when frozen, got: %s", out)
	}
}

func TestFormatDigestV2_ShadowCostVisible(t *testing.T) {
	out := FormatDigestV2(DigestV2{
		Cost: &DigestCost{ReservedUSD: 10, IncurredUSD: 8, ReconciledUSD: 8, ShadowUSD: 25, ShadowCeilingUSD: 20},
	})
	if !strings.Contains(out, "shadow $25.00") {
		t.Fatalf("digest must show shadow spend, got:\n%s", out)
	}
	if !strings.Contains(out, "CEILING BREACHED") {
		t.Fatalf("digest must flag a breached shadow ceiling, got:\n%s", out)
	}
	if !strings.Contains(out, "incurred $8.00") {
		t.Fatalf("digest must show incurred, got:\n%s", out)
	}
}
