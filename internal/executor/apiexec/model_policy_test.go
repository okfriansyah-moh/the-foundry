package apiexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelPolicyResolve(t *testing.T) {
	p := ModelPolicy{Models: map[string]map[string]string{
		"openai": {"default": "gpt-5.4", "frontend": "gpt-5.4-mini"},
	}}
	if got := p.Resolve("openai", "frontend"); got != "gpt-5.4-mini" {
		t.Fatalf("class-specific model = %q, want gpt-5.4-mini", got)
	}
	if got := p.Resolve("openai", "backend"); got != "gpt-5.4" {
		t.Fatalf("unmapped class should fall back to default, got %q", got)
	}
	if got := p.Resolve("openai", ""); got != "gpt-5.4" {
		t.Fatalf("empty class should use default, got %q", got)
	}
	if got := p.Resolve("unknown", "frontend"); got != "" {
		t.Fatalf("unknown provider should resolve to empty, got %q", got)
	}
}

func TestModelPolicyLoadShipped(t *testing.T) {
	p, err := LoadModelPolicy(filepath.Join("..", "..", "..", "config", "executor-models.yaml"))
	if err != nil {
		t.Fatalf("LoadModelPolicy: %v", err)
	}
	if p.Resolve("openai", "frontend") == "" {
		t.Fatal("shipped model policy missing openai/frontend mapping")
	}
}

func TestModelPolicyStrictRejectsUnknownKey(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("models: {}\nbogus: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModelPolicy(bad); err == nil {
		t.Fatal("expected strict-rejection error for unknown top-level key")
	}
}

func TestModelPolicyMissingFileIsNonFatal(t *testing.T) {
	p, err := LoadModelPolicy(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing model policy file must be non-fatal, got %v", err)
	}
	if p.Resolve("openai", "frontend") != "" {
		t.Fatal("empty policy should resolve to empty")
	}
}
