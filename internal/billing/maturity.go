package billing

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
)

// MaturityState is a profile's billing-autonomy maturity (docs/PLAN.md Task 83
// / EVO-10, Constitution C19). It is the gate that decides whether bounded
// billing implementation changes may run at Tier A2 instead of always H.
type MaturityState string

const (
	// MaturityImmature is the default: billing changes are always Tier H.
	MaturityImmature MaturityState = "immature"
	// MaturityMatured means a profile graduated on proven evidence with R4
	// human sign-off; bounded non-destructive, non-money-semantic billing
	// changes may run at A2.
	MaturityMatured MaturityState = "matured"
	// MaturityRevoked means a post-graduation billing incident revoked
	// maturity — back to H, plus a P1 (RevokeOnIncident).
	MaturityRevoked MaturityState = "revoked"
)

// MaturityEvidence is the real ledger/incident data the graduation evaluator
// scores, per admission-tiers.md §3's graduation-evidence list.
type MaturityEvidence struct {
	BillingCycles        int
	ChargesProcessed     int
	UnresolvedIncidents  int
	Chargebacks          int
	RefundRate           float64
	TestSuitePassed      bool
	IdempotencyProven    bool
	RecoveryTestRecorded bool
}

// MaturityCriteria is the graduation bar. The numeric bounds are conservative
// placeholders flagged for Blocker B6 (owner decision) — Placeholder is true
// until real numbers are set.
type MaturityCriteria struct {
	MinCycles     int
	MinCharges    int
	MaxRefundRate float64
	Placeholder   bool
}

// DefaultMaturityCriteria returns admission-tiers.md §3's stated bar with
// conservative B6 placeholder numbers (3 cycles, 10 charges, refund rate
// below 5%, zero unresolved incidents/chargebacks, all proofs present).
func DefaultMaturityCriteria() MaturityCriteria {
	return MaturityCriteria{MinCycles: 3, MinCharges: 10, MaxRefundRate: 0.05, Placeholder: true}
}

// Evaluate scores ev against c and returns whether every criterion is met plus
// the sorted list of missing criteria. Graduation is impossible while missing
// is non-empty (Task 83 acceptance).
func (c MaturityCriteria) Evaluate(ev MaturityEvidence) (matured bool, missing []string) {
	if ev.BillingCycles < c.MinCycles {
		missing = append(missing, "insufficient-billing-cycles")
	}
	if ev.ChargesProcessed < c.MinCharges {
		missing = append(missing, "insufficient-charges")
	}
	if ev.UnresolvedIncidents > 0 {
		missing = append(missing, "unresolved-incidents")
	}
	if ev.Chargebacks > 0 {
		missing = append(missing, "chargebacks-present")
	}
	if ev.RefundRate > c.MaxRefundRate {
		missing = append(missing, "refund-rate-too-high")
	}
	if !ev.TestSuitePassed {
		missing = append(missing, "test-suite-not-passing")
	}
	if !ev.IdempotencyProven {
		missing = append(missing, "idempotency-not-proven")
	}
	if !ev.RecoveryTestRecorded {
		missing = append(missing, "recovery-test-missing")
	}
	sort.Strings(missing)
	return len(missing) == 0, missing
}

// GraduationRecord is the signed record of a billing-maturity graduation. The
// first graduation requires R4 human sign-off (Signer must be set).
type GraduationRecord struct {
	ProfileID    string
	State        MaturityState
	Criteria     MaturityCriteria
	Evidence     MaturityEvidence
	Signer       string // R4 human sign-off; empty is a graduation error
	SignedAt     time.Time
	RevokeReason string
}

// Graduate evaluates ev and, if every criterion is met AND an R4 signer is
// provided, returns a Matured GraduationRecord. It fails closed otherwise:
// graduation is impossible on any missing criterion, and impossible without a
// human signer the first time.
func Graduate(profileID string, c MaturityCriteria, ev MaturityEvidence, signer string, now time.Time) (GraduationRecord, error) {
	matured, missing := c.Evaluate(ev)
	if !matured {
		return GraduationRecord{ProfileID: profileID, State: MaturityImmature, Criteria: c, Evidence: ev},
			fmt.Errorf("billing: graduation impossible — missing criteria: %s", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(signer) == "" {
		return GraduationRecord{ProfileID: profileID, State: MaturityImmature, Criteria: c, Evidence: ev},
			fmt.Errorf("billing: first graduation requires R4 human sign-off (signer is empty)")
	}
	return GraduationRecord{
		ProfileID: profileID, State: MaturityMatured, Criteria: c, Evidence: ev,
		Signer: signer, SignedAt: now.UTC(),
	}, nil
}

// RevokeResult carries a revocation and the P1 that must be raised.
type RevokeResult struct {
	Record   GraduationRecord
	RaiseP1  bool
	P1Reason string
}

// RevokeOnIncident revokes a graduated record on any post-graduation billing
// incident: maturity returns to H (MaturityRevoked) and a P1 is raised. It is
// the regression brake (Task 83 acceptance).
func RevokeOnIncident(rec GraduationRecord, incidentReason string) RevokeResult {
	rec.State = MaturityRevoked
	rec.RevokeReason = incidentReason
	return RevokeResult{
		Record:   rec,
		RaiseP1:  true,
		P1Reason: "billing maturity revoked by incident: " + incidentReason,
	}
}

// MoneySemanticTerms is the hard-pinned list of billing money-semantics that
// stay Tier H post-graduation unless mission pre-authorization exists
// (admission-tiers.md §3). Any change touching one of these is never a
// "bounded implementation change".
var MoneySemanticTerms = []string{
	"amount", "currency", "tax", "refund", "renewal", "cancellation",
	"proration", "trial", "migration", "provider", "payment-data",
}

// BillingChange describes one proposed billing change the v1.2 classifier
// tiers.
type BillingChange struct {
	// Description and Fields are scanned for money-semantic terms.
	Description string
	Fields      []string
	// Destructive changes are always H, matured or not.
	Destructive bool
	// MissionPreAuthorized is true when a mission explicitly pre-authorized a
	// money-semantic change — the only way one runs below H post-graduation.
	MissionPreAuthorized bool
}

// IsMoneySemantic reports whether the change touches any money-semantic term.
func (ch BillingChange) IsMoneySemantic() bool {
	hay := strings.ToLower(ch.Description + " " + strings.Join(ch.Fields, " "))
	for _, term := range MoneySemanticTerms {
		if strings.Contains(hay, term) {
			return true
		}
	}
	return false
}

// ClassifyBillingChange is billing classifier v1.2 (docs/PLAN.md Task 83): the
// graduated refinement of billing tiering. The plan-level admission classifier
// (internal/admission) still pins every EffectBilling to H as the safe default
// — this function applies only within a graduated profile's billing pipeline,
// deciding whether a specific bounded change may run at A2.
//
// Rules:
//   - Not Matured (immature or revoked) ⇒ H (default; graduation is the only
//     door to A2).
//   - Destructive ⇒ H, always.
//   - Money-semantic ⇒ H, UNLESS mission pre-authorization exists.
//   - Otherwise (bounded, non-destructive, non-money-semantic) ⇒ A2.
func ClassifyBillingChange(ch BillingChange, state MaturityState) admission.Tier {
	if state != MaturityMatured {
		return admission.TierH
	}
	if ch.Destructive {
		return admission.TierH
	}
	if ch.IsMoneySemantic() && !ch.MissionPreAuthorized {
		return admission.TierH
	}
	return admission.TierA2
}
