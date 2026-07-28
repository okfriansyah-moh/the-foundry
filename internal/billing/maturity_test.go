package billing

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
)

func maturedEvidence() MaturityEvidence {
	return MaturityEvidence{
		BillingCycles: 3, ChargesProcessed: 10, UnresolvedIncidents: 0, Chargebacks: 0,
		RefundRate: 0.01, TestSuitePassed: true, IdempotencyProven: true, RecoveryTestRecorded: true,
	}
}

func TestMaturityGraduation_HappyPath(t *testing.T) {
	rec, err := Graduate("personal", DefaultMaturityCriteria(), maturedEvidence(), "owner@example.com", time.Now())
	if err != nil {
		t.Fatalf("Graduate: %v", err)
	}
	if rec.State != MaturityMatured || rec.Signer == "" {
		t.Fatalf("expected matured+signed record, got %+v", rec)
	}
}

// TestMaturityGraduation_ImpossibleOnMissingCriterion is the acceptance: every
// individual missing criterion blocks graduation.
func TestMaturityGraduation_ImpossibleOnMissingCriterion(t *testing.T) {
	mutations := map[string]func(*MaturityEvidence){
		"too-few-cycles":  func(e *MaturityEvidence) { e.BillingCycles = 2 },
		"too-few-charges": func(e *MaturityEvidence) { e.ChargesProcessed = 9 },
		"unresolved":      func(e *MaturityEvidence) { e.UnresolvedIncidents = 1 },
		"chargeback":      func(e *MaturityEvidence) { e.Chargebacks = 1 },
		"refund-rate":     func(e *MaturityEvidence) { e.RefundRate = 0.20 },
		"no-tests":        func(e *MaturityEvidence) { e.TestSuitePassed = false },
		"no-idempotency":  func(e *MaturityEvidence) { e.IdempotencyProven = false },
		"no-recovery":     func(e *MaturityEvidence) { e.RecoveryTestRecorded = false },
	}
	for name, mut := range mutations {
		t.Run(name, func(t *testing.T) {
			ev := maturedEvidence()
			mut(&ev)
			if _, err := Graduate("p", DefaultMaturityCriteria(), ev, "owner", time.Now()); err == nil {
				t.Fatalf("graduation must be impossible when %q is missing", name)
			}
		})
	}
}

// TestMaturityGraduation_RequiresR4Signer proves the first graduation needs a
// human sign-off.
func TestMaturityGraduation_RequiresR4Signer(t *testing.T) {
	if _, err := Graduate("p", DefaultMaturityCriteria(), maturedEvidence(), "", time.Now()); err == nil {
		t.Fatal("graduation without an R4 signer must fail")
	}
}

// TestMaturityMoneySemanticStaysH is the core acceptance: post-graduation, the
// money-semantic list is provably H (unless mission pre-authorized), while a
// bounded non-money change is A2.
func TestMaturityMoneySemanticStaysH(t *testing.T) {
	for _, term := range MoneySemanticTerms {
		ch := BillingChange{Description: "change the " + term + " handling"}
		if got := ClassifyBillingChange(ch, MaturityMatured); got != admission.TierH {
			t.Fatalf("money-semantic %q must be H post-graduation, got %s", term, got)
		}
		// With mission pre-authorization, it may drop to A2.
		ch.MissionPreAuthorized = true
		if got := ClassifyBillingChange(ch, MaturityMatured); got != admission.TierA2 {
			t.Fatalf("money-semantic %q with mission pre-auth should be A2, got %s", term, got)
		}
	}
	// Bounded, non-destructive, non-money-semantic change → A2 post-graduation.
	bounded := BillingChange{Description: "refactor invoice email template rendering"}
	if got := ClassifyBillingChange(bounded, MaturityMatured); got != admission.TierA2 {
		t.Fatalf("bounded non-money billing change should be A2, got %s", got)
	}
	// Destructive change is always H.
	if got := ClassifyBillingChange(BillingChange{Description: "drop table", Destructive: true}, MaturityMatured); got != admission.TierH {
		t.Fatalf("destructive change must be H, got %s", got)
	}
}

// TestMaturityImmatureAndRevokedAreH proves that without graduation (or after
// revocation) every billing change is H.
func TestMaturityImmatureAndRevokedAreH(t *testing.T) {
	bounded := BillingChange{Description: "refactor invoice email template"}
	for _, st := range []MaturityState{MaturityImmature, MaturityRevoked} {
		if got := ClassifyBillingChange(bounded, st); got != admission.TierH {
			t.Fatalf("state %s must classify billing changes as H, got %s", st, got)
		}
	}
}

// TestMaturityRevocation proves a post-graduation incident revokes maturity
// back to H and raises a P1.
func TestMaturityRevocation(t *testing.T) {
	rec, err := Graduate("p", DefaultMaturityCriteria(), maturedEvidence(), "owner", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	res := RevokeOnIncident(rec, "duplicate charge on invoice 42")
	if res.Record.State != MaturityRevoked {
		t.Fatalf("incident must revoke maturity, got %s", res.Record.State)
	}
	if !res.RaiseP1 || res.P1Reason == "" {
		t.Fatal("revocation must raise a P1 with a reason")
	}
	// Post-revocation, billing changes are H again.
	if got := ClassifyBillingChange(BillingChange{Description: "tweak receipt copy"}, res.Record.State); got != admission.TierH {
		t.Fatalf("post-revocation billing change must be H, got %s", got)
	}
}
