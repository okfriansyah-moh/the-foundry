package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flyAPIStub emulates the Fly API deploy/rollback surface the FlyAdapter calls,
// so the exact production code path is exercised in CI against a recorded API
// surface (docs/PLAN.md Task 125's "cassette/contract test"). It records the
// requests it received so the test can assert the adapter sent the right thing.
type flyAPIStub struct {
	server   *httptest.Server
	requests []string
	failNext bool
}

func newFlyAPIStub(t *testing.T) *flyAPIStub {
	t.Helper()
	s := &flyAPIStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		if s.failNext {
			s.failNext = false
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"deploy failed"}`))
			return
		}
		var req deployRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deployResponse{App: req.App, URL: s.server.URL + "/healthz", Ref: req.Image})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func TestFlyAdapter_DeployHealthRollback_Contract(t *testing.T) {
	stub := newFlyAPIStub(t)
	adapter := FlyAdapter{Token: "tok", BaseURL: stub.server.URL, HTTP: stub.server.Client()}
	ctx := context.Background()

	rec, err := adapter.DeployProduction(ctx, "Demo", "img-1")
	if err != nil {
		t.Fatalf("DeployProduction: %v", err)
	}
	if rec.Product != "foundry-demo" || rec.Environment != "production" {
		t.Fatalf("record = %+v, want product foundry-demo/production", rec)
	}
	if rec.URL == "" {
		t.Fatal("deploy record must carry the real reachable URL")
	}
	// Health of the real returned URL passes.
	if err := adapter.Health(ctx, rec.URL); err != nil {
		t.Fatalf("Health of healthy deploy: %v", err)
	}
	// Rollback re-deploys the previous ref.
	if _, err := adapter.Rollback(ctx, "Demo", "img-0"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := RehearseRollback(ctx, adapter, "Demo", "img-1", "img-0"); err != nil {
		t.Fatalf("RehearseRollback: %v", err)
	}
}

func TestFlyAdapter_DeployFailureIsRealError(t *testing.T) {
	stub := newFlyAPIStub(t)
	stub.failNext = true
	adapter := FlyAdapter{Token: "tok", BaseURL: stub.server.URL, HTTP: stub.server.Client()}
	if _, err := adapter.DeployProduction(context.Background(), "Demo", "img-1"); err == nil {
		t.Fatal("a non-2xx deploy response must be a real error, not a fabricated success")
	} else if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error should carry the remote status, got %v", err)
	}
}

func TestFlyAdapter_HealthFailsOnUnhealthy(t *testing.T) {
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(unhealthy.Close)
	adapter := FlyAdapter{Token: "tok", HTTP: unhealthy.Client()}
	if err := adapter.Health(context.Background(), unhealthy.URL); err == nil {
		t.Fatal("Health must fail on a non-2xx response — an unhealthy deploy is never assumed healthy")
	}
}

func TestFlyAdapter_MissingTokenRefused(t *testing.T) {
	adapter := FlyAdapter{BaseURL: "http://example.invalid"}
	if _, err := adapter.DeployProduction(context.Background(), "Demo", "img"); err == nil {
		t.Fatal("a missing token must be refused before any network call")
	}
}
