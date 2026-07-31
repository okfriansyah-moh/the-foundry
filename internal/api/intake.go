package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

// docs/PLAN.md Task 111 (INT-03): the HTTP surface over the intake pipeline.
// Transport only — the pipeline itself makes no authority decision, never sets
// a declared tier and never approves a plan it generated (Constitution C6). The
// dependency is optional: a daemon that has not wired the pipeline serves 503
// on these routes rather than failing construction.

// IntakeService is the narrow seam internal/api needs over the intake pipeline.
// *intake.Pipeline supplies Start/Resume; an intake.Store supplies the reads.
type IntakeService interface {
	Start(ctx context.Context, in intake.StartInput) (intake.Run, error)
	Resume(ctx context.Context, runID string) (intake.Run, error)
	GetRun(ctx context.Context, runID string) (intake.Run, error)
	ListRuns(ctx context.Context, limit int) ([]intake.Run, error)
}

// intakeStartRequest is the wire body of POST /v1/intake. Idea text is
// untrusted data; a budget in the body is a request, clamped by the pipeline's
// budget gate, never a grant.
type intakeStartRequest struct {
	Idea   string  `json:"idea"`
	Budget float64 `json:"budget"`
}

// handleCreateIntake implements POST /v1/intake.
func (s *Server) handleCreateIntake(w http.ResponseWriter, r *http.Request) {
	if s.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake pipeline not configured")
		return
	}
	var req intakeStartRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Idea == "" {
		writeError(w, http.StatusBadRequest, "idea text is required")
		return
	}
	run, err := s.deps.Intake.Start(r.Context(), intake.StartInput{
		Idea:   req.Idea,
		Budget: req.Budget,
		Origin: intake.Origin{Channel: "api", PrincipalID: principalFromContext(r.Context())},
	})
	if err != nil {
		s.logger().Error("intake start", "error", err)
		writeError(w, http.StatusBadRequest, "intake start refused")
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// handleShowIntake implements GET /v1/intake/{id}.
func (s *Server) handleShowIntake(w http.ResponseWriter, r *http.Request) {
	if s.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake pipeline not configured")
		return
	}
	run, err := s.deps.Intake.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, intake.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "intake run not found")
			return
		}
		s.logger().Error("intake show", "error", err)
		writeError(w, http.StatusInternalServerError, "intake read failed")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleResumeIntake implements POST /v1/intake/{id}/resume.
func (s *Server) handleResumeIntake(w http.ResponseWriter, r *http.Request) {
	if s.deps.Intake == nil {
		writeError(w, http.StatusServiceUnavailable, "intake pipeline not configured")
		return
	}
	run, err := s.deps.Intake.Resume(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, intake.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "intake run not found")
			return
		}
		s.logger().Error("intake resume", "error", err)
		writeError(w, http.StatusBadRequest, "intake resume refused")
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}
