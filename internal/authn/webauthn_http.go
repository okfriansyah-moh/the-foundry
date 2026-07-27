package authn

import (
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// WebAuthnHTTP bundles the WebAuthn registration and assertion HTTP
// endpoints (docs/PLAN.md Task 25 Step 2: "WebAuthn (go-webauthn)
// registration + assertion endpoints"). Every endpoint requires a valid
// session JWT and derives the principal from it — never from the request
// body or query string — so a caller can never register a credential for,
// or begin a login ceremony as, a principal other than the one their own
// session token was issued to.
type WebAuthnHTTP struct {
	SessionPub *ecdsa.PublicKey
	Service    *Service
}

type webauthnBeginResponse struct {
	SessionID string          `json:"session_id"`
	Options   json.RawMessage `json:"options"`
}

// BeginRegistration implements POST /v1/webauthn/register/begin.
func (h *WebAuthnHTTP) BeginRegistration(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(h.SessionPub, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	optionsJSON, sessionID, err := h.Service.BeginRegistration(principal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not begin registration")
		return
	}
	writeJSON(w, http.StatusOK, webauthnBeginResponse{SessionID: sessionID, Options: optionsJSON})
}

// FinishRegistration implements POST
// /v1/webauthn/register/finish?session_id=<id>, body: the browser's raw
// attestation response.
func (h *WebAuthnHTTP) FinishRegistration(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(h.SessionPub, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}
	cred, err := h.Service.FinishRegistration(principal, sessionID, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "registration verification failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"credential_id": base64.RawURLEncoding.EncodeToString(cred.ID),
	})
}

// BeginLogin implements POST /v1/webauthn/login/begin: it hands out the
// sessionID + assertion options a caller then completes against
// ApproveHandler (POST /v1/plans/{id}/approve carries the resulting
// assertion, not a separate finish-login endpoint — the approval and the
// step-up ceremony are one action, per docs/PLAN.md Task 25 Step 2).
func (h *WebAuthnHTTP) BeginLogin(w http.ResponseWriter, r *http.Request) {
	principal, err := principalFromRequest(h.SessionPub, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired session")
		return
	}
	optionsJSON, sessionID, err := h.Service.BeginLogin(principal)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not begin login")
		return
	}
	writeJSON(w, http.StatusOK, webauthnBeginResponse{SessionID: sessionID, Options: optionsJSON})
}
