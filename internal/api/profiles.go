package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

// handleProfileList implements GET /v1/profiles, mirroring
// `foundry profile list` (cmd/foundry/profile.go).
func (s *Server) handleProfileList(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.deps.Profiles.List(r.Context())
	if err != nil {
		s.logger().Error("api: list profiles failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list profiles")
		return
	}
	writeJSON(w, http.StatusOK, profiles)
}

// handleProfileShow implements GET /v1/profiles/{id}, mirroring
// `foundry profile show <id>` (cmd/foundry/profile.go).
func (s *Server) handleProfileShow(w http.ResponseWriter, r *http.Request) {
	p, err := s.deps.Profiles.Load(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// createProfileRequest is POST /v1/profiles's body. Config must already
// match config/schemas/profile.schema.json — profile.Store.Save validates
// it (Task 21) before any write.
type createProfileRequest struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Kind   string          `json:"kind"`
	OrgID  string          `json:"org_id,omitempty"`
	Config json.RawMessage `json:"config"`
}

// handleProfileCreate implements POST /v1/profiles, mirroring
// `foundry profile create` (cmd/foundry/profile.go), including that
// command's same placeholder-policy-digest decision (Task 21: a real
// policy_digest requires Task 22's compiler bound to a specific profile,
// which no profile-create call site produces yet; the sha256 of the
// profile's own canonical config bytes is deterministic and changes
// whenever config changes, which is what Task 21 recorded as this
// call site's placeholder pending real compiler wiring).
func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var req createProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	if len(req.Config) == 0 {
		writeError(w, http.StatusBadRequest, "config is required")
		return
	}

	p := &profile.Profile{
		ID:           req.ID,
		Name:         req.Name,
		Kind:         profile.Kind(req.Kind),
		Config:       req.Config,
		PolicyDigest: placeholderPolicyDigest(req.Config),
	}
	if req.OrgID != "" {
		p.OrgID = &req.OrgID
	}

	// Validate explicitly first so a schema/field error (safe, user-facing —
	// the same text `foundry profile create` would print) is distinguished
	// from a raw-store failure, whose detail must not be echoed to the
	// client (OWASP A05).
	if err := p.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "profile create: "+err.Error())
		return
	}
	if err := s.deps.Profiles.Save(r.Context(), p); err != nil {
		if errors.Is(err, profile.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "profile already exists")
			return
		}
		s.logger().Error("api: save profile failed", "profile_id", p.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save profile")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func placeholderPolicyDigest(config []byte) string {
	sum := sha256.Sum256(config)
	return hex.EncodeToString(sum[:])
}
