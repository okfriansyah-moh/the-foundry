package authn

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// AuthMethodOIDC is the approval method recorded when a valid session JWT
// (OIDC-verified principal) authorized the approval but no WebAuthn
// step-up was required for this plan.
const AuthMethodOIDC = "oidc"

// AuthMethodOIDCWebAuthn is the approval method recorded when strong auth
// — a valid session JWT plus a fresh WebAuthn assertion — authorized the
// approval (docs/PLAN.md Task 25 Step 3 / Constitution C12).
const AuthMethodOIDCWebAuthn = "oidc+webauthn"

// PlanContext is the tier/profile classification the approval endpoint
// needs to decide whether strong auth is required for a plan
// (docs/PLAN.md Task 25 Step 2: "Decision.Tier==H (reuse Task 7's
// admission.Tier) or profile=organization (reuse Task 21's profile.Kind)").
type PlanContext struct {
	Tier    admission.Tier
	Profile profile.Kind
}

// RequiresStrongAuth reports whether ctx requires a fresh WebAuthn
// assertion before an approval is accepted. High tier or an organization
// profile is independently sufficient — this is an OR, not an AND, so
// neither condition can be used to talk the other one down.
func (c PlanContext) RequiresStrongAuth() bool {
	return c.Tier == admission.TierH || c.Profile == profile.Organization
}

// PlanContextResolver looks up the tier/profile classification for
// planID. It is supplied by the caller that wires up ApproveHandler (a
// trusted server-side component that has already run admission for this
// plan), never derived from the untrusted request body — a client that
// could self-declare "this plan is personal-profile, low tier" would
// otherwise be able to talk its way out of step-up.
type PlanContextResolver func(ctx context.Context, planID string) (PlanContext, error)

// ApproveHandler implements POST /v1/plans/{id}/approve (docs/PLAN.md
// Task 25 Step 2): it requires a valid session JWT on every call, and
// additionally requires a fresh WebAuthn assertion whenever
// PlanContext.RequiresStrongAuth() is true for the target plan. There is
// no fallback path that accepts the approval when WebAuthn verification
// itself fails or is unavailable — every non-nil error from the WebAuthn
// service ends in a 403, never a silent allow.
type ApproveHandler struct {
	SessionPub     *ecdsa.PublicKey
	WebAuthn       *Service
	Store          *provenance.Store
	SigningKey     ed25519.PrivateKey
	ResolveContext PlanContextResolver
	// Logger records server-side detail for failures whose full error
	// text must not be echoed back to the client (docs/PLAN.md Task 25 /
	// OWASP A05: don't leak internal error detail in HTTP responses).
	// Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

func (h *ApproveHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

type approveRequest struct {
	// WebAuthnSessionID is the sessionID returned by Service.BeginLogin,
	// required only when the plan's PlanContext.RequiresStrongAuth().
	WebAuthnSessionID string `json:"webauthn_session_id,omitempty"`
	// WebAuthnAssertion is the browser's raw PublicKeyCredential
	// assertion response JSON, forwarded byte-for-byte to
	// Service.FinishLogin.
	WebAuthnAssertion json.RawMessage `json:"webauthn_assertion,omitempty"`
}

type approveResponse struct {
	PlanID    string                `json:"plan_id"`
	Approver  provenance.Approver   `json:"approver"`
	Approvers []provenance.Approver `json:"approvers"`
}

// ServeHTTP implements http.Handler.
func (h *ApproveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	if planID == "" {
		writeError(w, http.StatusBadRequest, "missing plan id")
		return
	}

	principal, err := principalFromRequest(h.SessionPub, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}

	planCtx, err := h.ResolveContext(r.Context(), planID)
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}

	req, err := decodeApproveRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	method, assertionHash, status, msg := h.stepUp(principal, planCtx, req)
	if msg != "" {
		writeError(w, status, msg)
		return
	}

	approver := provenance.Approver{
		Principal:     principal,
		Method:        method,
		At:            time.Now().UTC(),
		AssertionHash: assertionHash,
	}

	approved, err := h.Store.AddApprover(r.Context(), planID, h.SigningKey, approver)
	if err != nil {
		h.logger().Error("authn: record approval failed", "plan_id", planID, "error", err)
		writeError(w, http.StatusConflict, "could not record approval")
		return
	}

	writeJSON(w, http.StatusOK, approveResponse{
		PlanID:    planID,
		Approver:  approver,
		Approvers: approved.Approvers(),
	})
}

// stepUp decides the approval method for a request, enforcing step-up
// when planCtx.RequiresStrongAuth(): a missing or invalid WebAuthn
// assertion returns a non-empty msg/status and must abort the request —
// there is no branch here that still returns a usable method after a
// WebAuthn failure.
func (h *ApproveHandler) stepUp(principal string, planCtx PlanContext, req approveRequest) (method, assertionHash string, status int, msg string) {
	if !planCtx.RequiresStrongAuth() {
		return AuthMethodOIDC, "", 0, ""
	}
	if req.WebAuthnSessionID == "" || len(req.WebAuthnAssertion) == 0 {
		return "", "", http.StatusForbidden, "high-risk approval requires a fresh WebAuthn assertion"
	}
	assertion, err := h.WebAuthn.FinishLogin(principal, req.WebAuthnSessionID, bytes.NewReader(req.WebAuthnAssertion))
	if err != nil {
		return "", "", http.StatusForbidden, "webauthn assertion verification failed"
	}
	return AuthMethodOIDCWebAuthn, assertion.AssertionHash, 0, ""
}

func decodeApproveRequest(r *http.Request) (approveRequest, error) {
	var req approveRequest
	if r.Body == nil {
		return req, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return approveRequest{}, fmt.Errorf("authn: decode approve request: %w", err)
	}
	return req, nil
}

func principalFromRequest(pub *ecdsa.PublicKey, r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", fmt.Errorf("authn: missing bearer session token")
	}
	token := strings.TrimPrefix(auth, prefix)
	return VerifySession(pub, []byte(token))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
