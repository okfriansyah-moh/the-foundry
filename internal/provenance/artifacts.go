package provenance

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// PlanSubmission is the first artifact in the provenance chain: the raw
// claim that a plan digest was submitted by some principal, from some
// source, at some time (docs/foundry/docs/security/approval-and-provenance.md
// §1).
type PlanSubmission struct {
	Digest    string    `json:"digest"`
	Source    string    `json:"source"`
	Submitter string    `json:"submitter"`
	At        time.Time `json:"at"`
}

// Scope declares the repositories, paths, and branches an ApprovedPlan is
// bound to.
type Scope struct {
	Repositories []string `json:"repositories"`
	Paths        []string `json:"paths"`
	Branches     []string `json:"branches"`
}

// BudgetEnvelope caps spend for the approved plan and for a single
// workflow run within it.
type BudgetEnvelope struct {
	MonthlyUSD  float64 `json:"monthly_usd"`
	WorkflowUSD float64 `json:"workflow_usd"`
}

// Approver is one recorded actual approval.
type Approver struct {
	Principal string    `json:"principal"`
	Method    string    `json:"method"`
	At        time.Time `json:"at"`
	// AssertionHash is the sha256 hex digest of the raw strong-auth
	// assertion (e.g. a WebAuthn assertion response) that authorized this
	// approval, set only when Method requires one
	// (docs/PLAN.md Task 25 / Constitution C12). Empty for approval
	// methods that carry no such assertion (e.g. AuthMethodEd25519Local).
	AssertionHash string `json:"assertion_hash,omitempty"`
}

// AuthMethodEd25519Local is the only strong-auth method this v0 supports
// (docs/PLAN.md Task 8 Step 2). OIDC/WebAuthn are Task 25.
const AuthMethodEd25519Local = "ed25519-local"

// ApprovedPlan is the terminal artifact of the provenance chain. Every
// field is unexported and reachable only through a getter or through
// NewApprovedPlan: this makes "Granted is always Requested intersected with
// policy" an invariant of the type itself, not a runtime check that some
// caller could route around (docs/PLAN.md Task 8 Step 6, Constitution C7
// rule 2 — "the plan may request permissions; it must never grant them").
type ApprovedPlan struct {
	planID              string
	planDigest          string
	creatorPrincipal    string
	submittingPrincipal string
	classifierVersion   string
	declared            []plan.Effect
	requested           []plan.Permission
	granted             []plan.Permission
	scope               Scope
	riskTier            string
	budgetEnvelope      BudgetEnvelope
	dataClass           string
	approvers           []Approver
	authMethod          string
	approvedAt          time.Time
	expiresAt           time.Time
	revoked             bool
	revokedBy           string
	revocationReason    string
	signature           []byte
}

// ApprovedPlanInput is the constructor argument for NewApprovedPlan. It
// intentionally has no Granted field — Granted is always computed, never
// supplied.
type ApprovedPlanInput struct {
	PlanID              string
	PlanDigest          string
	CreatorPrincipal    string
	SubmittingPrincipal string
	ClassifierVersion   string
	Declared            []plan.Effect
	Requested           []plan.Permission
	Scope               Scope
	RiskTier            admission.Tier
	BudgetEnvelope      BudgetEnvelope
	DataClass           string
	Approvers           []Approver
	ApprovedAt          time.Time
	ExpiresAt           time.Time
}

// NewApprovedPlan builds an unsigned ApprovedPlan, computing Granted as
// Requested ∩ allow (Constitution C7 rule 2). The result still has no
// signature; call Sign before it can pass Verify, Insert, or Load.
func NewApprovedPlan(in ApprovedPlanInput, allow AllowList) (*ApprovedPlan, error) {
	if in.PlanID == "" {
		return nil, fmt.Errorf("provenance: missing plan_id")
	}
	if in.PlanDigest == "" {
		return nil, fmt.Errorf("provenance: missing plan_digest")
	}
	return &ApprovedPlan{
		planID:              in.PlanID,
		planDigest:          in.PlanDigest,
		creatorPrincipal:    in.CreatorPrincipal,
		submittingPrincipal: in.SubmittingPrincipal,
		classifierVersion:   in.ClassifierVersion,
		declared:            append([]plan.Effect{}, in.Declared...),
		requested:           append([]plan.Permission{}, in.Requested...),
		granted:             allow.Intersect(in.Requested),
		scope:               in.Scope,
		riskTier:            in.RiskTier.String(),
		budgetEnvelope:      in.BudgetEnvelope,
		dataClass:           in.DataClass,
		approvers:           append([]Approver{}, in.Approvers...),
		authMethod:          AuthMethodEd25519Local,
		approvedAt:          in.ApprovedAt,
		expiresAt:           in.ExpiresAt,
		revoked:             false,
	}, nil
}

// PlanID returns the approved plan's id.
func (a *ApprovedPlan) PlanID() string { return a.planID }

// PlanDigest returns the sha256 hex digest the approval is bound to.
func (a *ApprovedPlan) PlanDigest() string { return a.planDigest }

// RiskTier returns the classifier-assigned tier label ("A0".."H").
func (a *ApprovedPlan) RiskTier() string { return a.riskTier }

// Requested returns a copy of the requested permissions.
func (a *ApprovedPlan) Requested() []plan.Permission {
	return append([]plan.Permission{}, a.requested...)
}

// Granted returns a copy of the granted permissions — always a subset of
// Requested, by construction.
func (a *ApprovedPlan) Granted() []plan.Permission {
	return append([]plan.Permission{}, a.granted...)
}

// Approvers returns a copy of the recorded approvals.
func (a *ApprovedPlan) Approvers() []Approver {
	return append([]Approver{}, a.approvers...)
}

// Revoked reports whether this plan has been revoked. Store.Load enforces
// this — see docs/PLAN.md Task 24 — so callers do not need to re-check it
// themselves after a successful Load.
func (a *ApprovedPlan) Revoked() bool { return a.revoked }

// RevokedBy returns the principal that revoked this plan, empty if never
// revoked.
func (a *ApprovedPlan) RevokedBy() string { return a.revokedBy }

// RevocationReason returns the recorded reason for revocation, empty if
// never revoked.
func (a *ApprovedPlan) RevocationReason() string { return a.revocationReason }

// ExpiresAt returns the expiry time. Store.Load enforces this — see
// docs/PLAN.md Task 24 — so callers do not need to re-check it themselves
// after a successful Load.
func (a *ApprovedPlan) ExpiresAt() time.Time { return a.expiresAt }

// approvedPlanWire is the full JSON wire/storage representation of an
// ApprovedPlan, including the signature. Field order is fixed by struct
// declaration order, which is what makes encoding/json's output
// deterministic given the same values.
type approvedPlanWire struct {
	PlanID              string            `json:"plan_id"`
	PlanDigest          string            `json:"plan_digest"`
	CreatorPrincipal    string            `json:"creator_principal"`
	SubmittingPrincipal string            `json:"submitting_principal"`
	ClassifierVersion   string            `json:"classifier_version"`
	Declared            []plan.Effect     `json:"declared_effects"`
	Requested           []plan.Permission `json:"requested_permissions"`
	Granted             []plan.Permission `json:"granted_permissions"`
	Scope               Scope             `json:"scope"`
	RiskTier            string            `json:"risk_tier"`
	BudgetEnvelope      BudgetEnvelope    `json:"budget_envelope"`
	DataClass           string            `json:"data_classification"`
	Approvers           []Approver        `json:"approvers"`
	AuthMethod          string            `json:"auth_method"`
	ApprovedAt          time.Time         `json:"approved_at"`
	ExpiresAt           time.Time         `json:"expires_at"`
	Revoked             bool              `json:"revoked"`
	RevokedBy           string            `json:"revoked_by,omitempty"`
	RevocationReason    string            `json:"revocation_reason,omitempty"`
	Signature           string            `json:"signature,omitempty"` // base64
}

func (a *ApprovedPlan) toWire() approvedPlanWire {
	return approvedPlanWire{
		PlanID:              a.planID,
		PlanDigest:          a.planDigest,
		CreatorPrincipal:    a.creatorPrincipal,
		SubmittingPrincipal: a.submittingPrincipal,
		ClassifierVersion:   a.classifierVersion,
		Declared:            a.declared,
		Requested:           a.requested,
		Granted:             a.granted,
		Scope:               a.scope,
		RiskTier:            a.riskTier,
		BudgetEnvelope:      a.budgetEnvelope,
		DataClass:           a.dataClass,
		Approvers:           a.approvers,
		AuthMethod:          a.authMethod,
		ApprovedAt:          a.approvedAt,
		ExpiresAt:           a.expiresAt,
		Revoked:             a.revoked,
		RevokedBy:           a.revokedBy,
		RevocationReason:    a.revocationReason,
	}
}

// MarshalJSON encodes the full wire representation, including the
// signature (base64) when present. This is what gets persisted to the
// approved_plans row and what plan verify reads back.
func (a *ApprovedPlan) MarshalJSON() ([]byte, error) {
	w := a.toWire()
	if len(a.signature) > 0 {
		w.Signature = encodeSignature(a.signature)
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes the full wire representation produced by
// MarshalJSON, including the signature. Callers that need a verified
// ApprovedPlan must still call Verify (or go through Store.Load, which
// always does) — unmarshaling alone performs no cryptographic check.
func (a *ApprovedPlan) UnmarshalJSON(data []byte) error {
	var w approvedPlanWire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("provenance: unmarshal ApprovedPlan: %w", err)
	}
	sig, err := decodeSignature(w.Signature)
	if err != nil {
		return err
	}
	*a = ApprovedPlan{
		planID:              w.PlanID,
		planDigest:          w.PlanDigest,
		creatorPrincipal:    w.CreatorPrincipal,
		submittingPrincipal: w.SubmittingPrincipal,
		classifierVersion:   w.ClassifierVersion,
		declared:            w.Declared,
		requested:           w.Requested,
		granted:             w.Granted,
		scope:               w.Scope,
		riskTier:            w.RiskTier,
		budgetEnvelope:      w.BudgetEnvelope,
		dataClass:           w.DataClass,
		approvers:           w.Approvers,
		authMethod:          w.AuthMethod,
		approvedAt:          w.ApprovedAt,
		expiresAt:           w.ExpiresAt,
		revoked:             w.Revoked,
		revokedBy:           w.RevokedBy,
		revocationReason:    w.RevocationReason,
		signature:           sig,
	}
	return nil
}
