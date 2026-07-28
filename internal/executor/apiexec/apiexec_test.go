package apiexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

func TestGuardDataClass(t *testing.T) {
	cases := []struct {
		name      string
		dataClass string
		granted   []string
		wantErr   bool
	}{
		{"non-customer always allowed", "code", nil, false},
		{"empty class allowed", "", nil, false},
		{"customer to ungranted provider refused", "customer", nil, true},
		{"customer-pii to ungranted refused", "customer-pii", []string{"code"}, true},
		{"customer to granted provider allowed", "customer", []string{"customer"}, false},
		{"customer-pii to customer-granted allowed", "customer-pii", []string{"customer"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := GuardDataClass("openai", tc.dataClass, tc.granted)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GuardDataClass(%q, %v) err=%v, wantErr=%v", tc.dataClass, tc.granted, err, tc.wantErr)
			}
		})
	}
}

// TestCostMeteringInSummary proves pricing_version + cost are metered per call
// and surfaced as telemetry (local=zero is representable).
func TestCostMeteringInSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer srv.Close()

	a := New(Config{Provider: "test", BaseURL: srv.URL, Model: "m", PricingVersion: "pv-1", CostPerCallUSD: 0.02})
	ws := worktree.Workspace{Path: t.TempDir()}
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hi"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"pricing_version=pv-1", "cost_usd=0.0200", "prompt_tokens=10"} {
		if !strings.Contains(summary.ExitNotes, want) {
			t.Fatalf("ExitNotes missing %q: %s", want, summary.ExitNotes)
		}
	}
}

// TestZeroCostLocal proves a zero-cost (local) provider is representable.
func TestZeroCostLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	a := New(Config{Provider: "local", BaseURL: srv.URL, Model: "m", PricingVersion: "local-zero", CostPerCallUSD: 0})
	ws := worktree.Workspace{Path: t.TempDir()}
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hi"}); err != nil {
		t.Fatal(err)
	}
	summary, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.ExitNotes, "cost_usd=0.0000") {
		t.Fatalf("local cost should be zero: %s", summary.ExitNotes)
	}
}

// TestModelPerTaskClassReachesRequest proves the config-driven per-task-class
// model policy actually reaches the outbound request body (Task 79 EVO-06):
// a frontend-class packet uses the class-specific model, an unmapped class
// uses the provider default.
func TestModelPerTaskClassReachesRequest(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	policy := ModelPolicy{Models: map[string]map[string]string{
		"openai": {"default": "gpt-5.4", "frontend": "gpt-5.4-mini"},
	}}
	cfg := Config{Provider: "openai", BaseURL: srv.URL, Model: "gpt-5.4", ModelPolicy: policy}

	run := func(class string) string {
		a := New(cfg)
		ws := worktree.Workspace{Path: t.TempDir()}
		if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hi", Class: class}); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if _, err := a.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return gotModel
	}

	if m := run("frontend"); m != "gpt-5.4-mini" {
		t.Fatalf("frontend class should use gpt-5.4-mini, request used %q", m)
	}
	if m := run("backend"); m != "gpt-5.4" {
		t.Fatalf("unmapped class should use default gpt-5.4, request used %q", m)
	}
}
