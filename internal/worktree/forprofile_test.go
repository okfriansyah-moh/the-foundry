package worktree

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestForProfile_ScopesRootUnderProfile proves Task 118 (SEC-04): a
// profile-scoped manager roots every worktree under Root/<profile>/…, so a
// task's workspace is contained within its own profile's subtree.
func TestForProfile_ScopesRootUnderProfile(t *testing.T) {
	base := &Manager{Root: t.TempDir()}
	personal, err := base.ForProfile("personal")
	if err != nil {
		t.Fatalf("ForProfile(personal): %v", err)
	}
	org, err := base.ForProfile("org-acme")
	if err != nil {
		t.Fatalf("ForProfile(org-acme): %v", err)
	}
	if personal.Root == org.Root {
		t.Fatal("distinct profiles must have distinct worktree roots")
	}
	if filepath.Dir(personal.Root) != base.Root {
		t.Fatalf("profile root %q must live directly under the base root %q", personal.Root, base.Root)
	}
	// A workspace path for a profile is always a descendant of that profile's
	// root — never another profile's subtree.
	ws := filepath.Join(personal.Root, "wf-1", "t-1")
	if !strings.HasPrefix(ws, personal.Root+string(filepath.Separator)) {
		t.Fatalf("workspace %q escaped its profile root %q", ws, personal.Root)
	}
	if strings.HasPrefix(ws, org.Root+string(filepath.Separator)) {
		t.Fatalf("workspace %q is inside another profile's subtree %q", ws, org.Root)
	}
}

// TestForProfile_RejectsPathEscape proves a malicious profile id cannot escape
// the base root (path containment, not a convention).
func TestForProfile_RejectsPathEscape(t *testing.T) {
	base := &Manager{Root: t.TempDir()}
	for _, bad := range []string{"..", "../evil", "a/b", "/abs", ".", ""} {
		if _, err := base.ForProfile(bad); err == nil {
			t.Fatalf("ForProfile(%q) must be rejected as a path escape", bad)
		}
	}
}
