package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

// handleApprovePlan implements POST /v1/plans/{id}/approve by delegating
// to internal/authn.ApproveHandler (Task 25) — this package adds no new
// approval/step-up logic of its own; it only supplies the
// PlanContextResolver Task 25 left as an explicit seam ("no full
// plan/profile lookup service exists yet (Task 36)").
func (s *Server) handleApprovePlan(w http.ResponseWriter, r *http.Request) {
	h := &authn.ApproveHandler{
		SessionPub:     s.deps.SessionPub,
		WebAuthn:       s.deps.WebAuthn,
		Store:          s.deps.Provenance,
		SigningKey:     s.deps.ApprovalSigningKey,
		ResolveContext: s.resolvePlanContext,
		Logger:         s.logger(),
	}
	h.ServeHTTP(w, r)
}

// resolvePlanContext implements authn.PlanContextResolver: it loads the
// already-approved plan (never trusting a client-declared tier/profile —
// see internal/authn.PlanContextResolver's own doc) and derives the
// classification ApproveHandler needs to decide whether WebAuthn step-up
// is required.
//
// Task 118 (SEC-04): the profile kind is read from the ApprovedPlan's own
// re-signed provenance field, not hardcoded to profile.Personal — so an
// organization-profile plan requires WebAuthn step-up even below tier H,
// closing the "Profile == Organization half of RequiresStrongAuth can never
// fire" gap the audit found.
func (s *Server) resolvePlanContext(ctx context.Context, planID string) (authn.PlanContext, error) {
	approved, err := s.deps.Provenance.Load(ctx, planID)
	if err != nil {
		return authn.PlanContext{}, fmt.Errorf("api: load approved plan %s: %w", planID, err)
	}
	tier, err := parseTier(approved.RiskTier())
	if err != nil {
		return authn.PlanContext{}, fmt.Errorf("api: plan %s: %w", planID, err)
	}
	return authn.PlanContext{Tier: tier, Profile: profile.Kind(approved.ProfileKind())}, nil
}

// parseTier reverses admission.Tier.String() ("A0".."H") back into a
// Tier value. internal/admission does not export a parser (only
// MarshalJSON/UnmarshalJSON round-trip through the JSON string form), so
// this is a small local inverse of that switch rather than a change to
// internal/admission (go-kernel-owned; this task adds no field or method
// there).
func parseTier(label string) (admission.Tier, error) {
	switch label {
	case "A0":
		return admission.TierA0, nil
	case "A1":
		return admission.TierA1, nil
	case "A2":
		return admission.TierA2, nil
	case "H":
		return admission.TierH, nil
	default:
		return 0, fmt.Errorf("api: unrecognized risk tier label %q", label)
	}
}
