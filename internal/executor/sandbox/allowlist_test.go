package sandbox

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeAllowlist(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allowlist.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write allowlist fixture: %v", err)
	}
	return path
}

func TestLoadEgressAllowlist_Valid(t *testing.T) {
	path := writeAllowlist(t, `
version: 1
allow:
  - host: api.anthropic.com
    port: 443
    reason: "claude-code's own provider endpoint"
`)
	a, err := LoadEgressAllowlist(path)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}
	if len(a.Allow) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(a.Allow))
	}
	if !a.Allows("api.anthropic.com", 443) {
		t.Errorf("expected allowlisted host:port to be allowed")
	}
	if !a.Allows("API.ANTHROPIC.COM", 443) {
		t.Errorf("expected host comparison to be case-insensitive")
	}
	if a.Allows("api.anthropic.com", 80) {
		t.Errorf("expected different port to be denied")
	}
	if a.Allows("evil.example.com", 443) {
		t.Errorf("expected unlisted host to be denied")
	}
}

func TestLoadEgressAllowlist_RejectsWildcardHost(t *testing.T) {
	path := writeAllowlist(t, `
version: 1
allow:
  - host: "*"
    port: 443
`)
	if _, err := LoadEgressAllowlist(path); err == nil {
		t.Fatalf("expected wildcard host to be rejected")
	}
}

func TestLoadEgressAllowlist_RejectsEmptyHost(t *testing.T) {
	path := writeAllowlist(t, `
version: 1
allow:
  - host: ""
    port: 443
`)
	if _, err := LoadEgressAllowlist(path); err == nil {
		t.Fatalf("expected empty host to be rejected")
	}
}

func TestLoadEgressAllowlist_RejectsBadPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 100000} {
		path := writeAllowlist(t, `
version: 1
allow:
  - host: example.com
    port: `+strconv.Itoa(port)+`
`)
		if _, err := LoadEgressAllowlist(path); err == nil {
			t.Errorf("port %d: expected out-of-range port to be rejected", port)
		}
	}
}

func TestLoadEgressAllowlist_RejectsUnsupportedVersion(t *testing.T) {
	path := writeAllowlist(t, `
version: 2
allow:
  - host: example.com
    port: 443
`)
	if _, err := LoadEgressAllowlist(path); err == nil {
		t.Fatalf("expected unsupported version to be rejected")
	}
}

func TestLoadEgressAllowlist_RejectsDuplicateEntry(t *testing.T) {
	path := writeAllowlist(t, `
version: 1
allow:
  - host: example.com
    port: 443
  - host: EXAMPLE.COM
    port: 443
`)
	if _, err := LoadEgressAllowlist(path); err == nil {
		t.Fatalf("expected duplicate (case-insensitive) entry to be rejected")
	}
}

func TestLoadEgressAllowlist_EmptyAllowIsValidButUseless(t *testing.T) {
	path := writeAllowlist(t, `
version: 1
allow: []
`)
	a, err := LoadEgressAllowlist(path)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist: %v", err)
	}
	if a.Allows("anything.example.com", 443) {
		t.Errorf("expected empty allowlist to allow nothing")
	}
}

func TestLoadEgressAllowlist_MissingFile(t *testing.T) {
	if _, err := LoadEgressAllowlist(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestLoadEgressAllowlist_RepoConfigFile(t *testing.T) {
	// This is the actual shipped config/sandbox-egress-allowlist.yaml —
	// prove it parses and validates as a real allowlist, not just the
	// fixtures above.
	path := repoConfigPath(t, "sandbox-egress-allowlist.yaml")
	a, err := LoadEgressAllowlist(path)
	if err != nil {
		t.Fatalf("LoadEgressAllowlist(%s): %v", path, err)
	}
	if !a.Allows("api.anthropic.com", 443) {
		t.Errorf("expected shipped allowlist to include api.anthropic.com:443")
	}
}

func repoConfigPath(t *testing.T, name string) string {
	t.Helper()
	// internal/executor/sandbox -> repo root is three levels up.
	return filepath.Join("..", "..", "..", "config", name)
}
