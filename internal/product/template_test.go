package product

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
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
	var enabled struct {
		Profile string   `yaml:"profile"`
		Agents  []string `yaml:"agents"`
	}
	if err := yaml.Unmarshal(raw, &enabled); err != nil {
		t.Fatalf("parse enabled.yaml: %v\n%s", err, raw)
	}
	if enabled.Profile != "personal-autonomous-venture" {
		t.Fatalf("enabled.yaml profile = %q, want personal-autonomous-venture", enabled.Profile)
	}
	if !slices.Contains(enabled.Agents, "reviewer") {
		t.Fatalf("enabled.yaml agents = %v, want reviewer present", enabled.Agents)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
