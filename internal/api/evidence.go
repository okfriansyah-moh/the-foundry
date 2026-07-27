package api

import (
	"errors"
	"net/http"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
)

// handleEvidenceShow implements GET /v1/evidence/{id}, mirroring
// `foundry evidence show <id>` (cmd/foundry/evidence.go): returns the
// stored manifest without re-verifying it.
func (s *Server) handleEvidenceShow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	bundle, err := s.deps.Evidence.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "evidence bundle not found")
		return
	}
	writeJSON(w, http.StatusOK, bundle.Manifest)
}

// handleEvidenceVerify implements GET /v1/evidence/{id}/verify, mirroring
// `foundry evidence verify <id>` (cmd/foundry/evidence.go): re-hashes
// every artifact and the manifest itself from bytes on disk.
func (s *Server) handleEvidenceVerify(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.deps.Evidence.Verify(id); err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, evidence.ErrVerifyFailed) {
			status = http.StatusConflict
		}
		writeError(w, status, "verification failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pass", "id": id})
}
