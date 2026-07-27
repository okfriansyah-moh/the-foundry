package api

import (
	"io"
	"net/http"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

// maxSubmitBytes bounds the request body handleSubmitPlan will read, so an
// unbounded/oversized upload cannot exhaust server memory (OWASP A05 —
// resource-limit input validation at the boundary). Plan files are
// Markdown+YAML front matter, not binary payloads; 1 MiB is generous.
const maxSubmitBytes = 1 << 20

// submitSource is the fixed PlanSubmission.Source value for plans
// submitted over this API (the CLI's equivalent records the local file
// path instead; there is no analogous path for an HTTP body).
const submitSource = "http"

// handleSubmitPlan implements POST /v1/plans (docs/PLAN.md Task 36):
// parses and validates the posted plan and returns the PlanSubmission
// artifact, mirroring `foundry plan submit` (cmd/foundry/plan_submit.go)
// byte-for-byte in behavior. It performs no admission decision and no
// persistence, exactly like its CLI counterpart — those happen at
// POST /v1/plans/{id}/approve.
//
// Submitter is the session's verified principal, never a client-supplied
// field — unlike the CLI's --submitter flag (a trusted local operator
// input), an HTTP client's claimed identity must come from the
// authenticated session, not the request body (OWASP A01).
func (s *Server) handleSubmitPlan(w http.ResponseWriter, r *http.Request) {
	principal := principalFromContext(r.Context())

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxSubmitBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return
	}
	if len(raw) > maxSubmitBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "plan body exceeds 1 MiB limit")
		return
	}

	doc, err := plan.ParseBytes(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "malformed plan: "+err.Error())
		return
	}

	submission := provenance.PlanSubmission{
		Digest:    doc.DigestHex(),
		Source:    submitSource,
		Submitter: principal,
		At:        time.Now().UTC(),
	}
	writeJSON(w, http.StatusOK, submission)
}
