package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAllowlist(t *testing.T, body string) Allowlist {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validation-allowlist.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write allowlist fixture: %v", err)
	}
	al, err := LoadAllowlist(path)
	if err != nil {
		t.Fatalf("LoadAllowlist: %v", err)
	}
	return al
}

func TestLoadAllowlistFromRepoConfig(t *testing.T) {
	al, err := LoadAllowlist("../../config/validation-allowlist.yaml")
	if err != nil {
		t.Fatalf("LoadAllowlist(repo config): %v", err)
	}
	for _, cmd := range []string{"go", "make", "npm", "pnpm", "pytest"} {
		if err := al.Check([]string{cmd, "version"}); err != nil {
			t.Errorf("expected %q allowed by repo config, got %v", cmd, err)
		}
	}
	if err := al.Check([]string{"curl", "http://evil"}); err == nil {
		t.Error("expected curl to be refused by repo config")
	}
}

func TestAllowlistCheckTable(t *testing.T) {
	al := writeAllowlist(t, `
commands:
  - go
  - make
scripts_dir: ./scripts/
`)

	tests := []struct {
		name    string
		argv    []string
		wantErr bool
	}{
		{"allowed plain command", []string{"go", "test", "./..."}, false},
		{"another allowed command", []string{"make", "test"}, false},
		{"unlisted command", []string{"curl", "http://evil"}, true},
		{"bash under scripts dir allowed", []string{"bash", "./scripts/check.sh"}, false},
		{"bash outside scripts dir refused", []string{"bash", "/etc/passwd"}, true},
		{"bash with -c refused", []string{"bash", "-c", "echo hi"}, true},
		{"empty argv refused", []string{}, true},
		{"semicolon-glued token refused", []string{";rm", "-rf", "/"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := al.Check(tt.argv)
			if tt.wantErr && err == nil {
				t.Errorf("Check(%v): expected error, got nil", tt.argv)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Check(%v): unexpected error: %v", tt.argv, err)
			}
		})
	}
}

func TestAllowlistScriptsDirTraversalRefused(t *testing.T) {
	al := writeAllowlist(t, `
commands: [go]
scripts_dir: ./scripts/
`)
	if err := al.Check([]string{"bash", "./scripts/../secrets.sh"}); err == nil {
		t.Error("expected path traversal out of scripts_dir to be refused")
	}
}
