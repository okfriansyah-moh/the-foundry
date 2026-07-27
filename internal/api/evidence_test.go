package api

import (
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
)

func (f *testFixture) putEvidence(t *testing.T) string {
	t.Helper()
	m := evidence.Manifest{WorkflowID: "wf-1", TaskID: "t1", CreatedAt: time.Now().UTC()}
	id, err := f.evidence.Put(evidence.Bundle{Manifest: m, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return id
}

func TestHandleEvidenceShow_Success(t *testing.T) {
	f := newTestFixture(t)
	id := f.putEvidence(t)

	rec := doRequest(f, "GET", "/v1/evidence/"+id, f.bearerToken(t), "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEvidenceShow_NotFoundIs404(t *testing.T) {
	f := newTestFixture(t)
	rec := doRequest(f, "GET", "/v1/evidence/does-not-exist", f.bearerToken(t), "")
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEvidenceVerify_Success(t *testing.T) {
	f := newTestFixture(t)
	id := f.putEvidence(t)

	rec := doRequest(f, "GET", "/v1/evidence/"+id+"/verify", f.bearerToken(t), "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEvidence_MissingSessionIs401(t *testing.T) {
	f := newTestFixture(t)
	id := f.putEvidence(t)

	rec := doRequest(f, "GET", "/v1/evidence/"+id, "", "")
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
