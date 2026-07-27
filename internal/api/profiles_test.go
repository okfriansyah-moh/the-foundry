package api

import (
	"encoding/json"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

const testProfileConfigJSON = `{"schema_version":1,"budget":{"max_usd":10}}`

func TestHandleProfileCreateAndShow(t *testing.T) {
	f := newTestFixture(t)
	bearer := f.bearerToken(t)

	body := `{"id":"p1","name":"Test Profile","kind":"personal","config":` + testProfileConfigJSON + `}`
	rec := doRequest(f, "POST", "/v1/profiles", bearer, body)
	if rec.Code != 201 {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(f, "GET", "/v1/profiles/p1", bearer, "")
	if rec.Code != 200 {
		t.Fatalf("show status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got profile.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "p1" || got.Kind != profile.Personal {
		t.Errorf("got profile %+v", got)
	}
}

func TestHandleProfileCreate_InvalidKindIs400(t *testing.T) {
	f := newTestFixture(t)
	body := `{"id":"p2","name":"Bad","kind":"not-a-kind","config":` + testProfileConfigJSON + `}`
	rec := doRequest(f, "POST", "/v1/profiles", f.bearerToken(t), body)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleProfileList(t *testing.T) {
	f := newTestFixture(t)
	bearer := f.bearerToken(t)
	body := `{"id":"p3","name":"Test","kind":"personal","config":` + testProfileConfigJSON + `}`
	if rec := doRequest(f, "POST", "/v1/profiles", bearer, body); rec.Code != 201 {
		t.Fatalf("create failed: %d %s", rec.Code, rec.Body.String())
	}

	rec := doRequest(f, "GET", "/v1/profiles", bearer, "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []*profile.Profile
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d profiles, want 1", len(got))
	}
}

func TestHandleProfileShow_NotFoundIs404(t *testing.T) {
	f := newTestFixture(t)
	rec := doRequest(f, "GET", "/v1/profiles/does-not-exist", f.bearerToken(t), "")
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
