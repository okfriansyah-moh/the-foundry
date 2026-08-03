package product

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstantiate(t *testing.T) {
	dir := t.TempDir()
	out, err := Instantiate(InstantiateOptions{
		Name:        "demo-product",
		Destination: dir,
	})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	mustExist(t, filepath.Join(out, "README.md"))
	mustExist(t, filepath.Join(out, "api", "server.go"))
	mustExist(t, filepath.Join(out, "frontend", "package.json"))
	enabledPath := filepath.Join(out, ".foundry", "skills", "enabled.yaml")
	mustExist(t, enabledPath)
	raw, err := os.ReadFile(enabledPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "profile: personal-autonomous-venture") || !strings.Contains(string(raw), "- reviewer") {
		t.Fatalf("enabled.yaml does not contain venture defaults:\n%s", raw)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
