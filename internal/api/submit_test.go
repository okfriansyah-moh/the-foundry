package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
)

const testPlanBody = `---
id: plan-1
title: Test Plan
version: "1"
tasks:
  - id: t1
    goal: do a thing
    validation_commands:
      - echo ok
---
## Notes
a test plan
`

func doRequest(f *testFixture, method, target, bearer, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

// TestHandleSubmitPlan_Success is this task's dogfood proof for the
// submit path: POSTing a valid plan body returns the same PlanSubmission
// shape `foundry plan submit` prints, with Submitter forced to the
// session principal rather than any client-supplied value.
func TestHandleSubmitPlan_Success(t *testing.T) {
	f := newTestFixture(t)
	rec := doRequest(f, "POST", "/v1/plans", f.bearerToken(t), testPlanBody)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got provenance.PlanSubmission
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Submitter != testPrincipal {
		t.Errorf("Submitter = %q, want %q (the session principal, not client-supplied)", got.Submitter, testPrincipal)
	}
	if got.Digest == "" {
		t.Error("Digest is empty")
	}
	if got.Source != submitSource {
		t.Errorf("Source = %q, want %q", got.Source, submitSource)
	}
}

func TestHandleSubmitPlan_MalformedPlanIs422(t *testing.T) {
	f := newTestFixture(t)
	rec := doRequest(f, "POST", "/v1/plans", f.bearerToken(t), "not a valid plan at all: [[[")

	if rec.Code != 422 {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSubmitPlan_MissingSessionIs401(t *testing.T) {
	f := newTestFixture(t)
	rec := doRequest(f, "POST", "/v1/plans", "", testPlanBody)

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

// TestHandleSubmitPlan_PDPDeniedIs403 proves the PDP layer is real and
// load-bearing, not a pass-through: a valid session with a denying
// Decider still gets rejected before the handler body ever runs.
func TestHandleSubmitPlan_PDPDeniedIs403(t *testing.T) {
	f := newTestFixture(t)
	f.decider.deny = true
	rec := doRequest(f, "POST", "/v1/plans", f.bearerToken(t), testPlanBody)

	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSubmitPlan_OversizedBodyIs413(t *testing.T) {
	f := newTestFixture(t)
	huge := strings.Repeat("a", maxSubmitBytes+1)
	rec := doRequest(f, "POST", "/v1/plans", f.bearerToken(t), huge)

	if rec.Code != 413 {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
}
