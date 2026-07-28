package product

import (
	"os"
	"path/filepath"
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
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
