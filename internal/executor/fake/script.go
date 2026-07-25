package fake

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Patch is one file write applied inside the workspace during Run.
type Patch struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

// Script is the deterministic behavior a fake executor plays back. Claimed
// and ExitNotes populate the resulting executor.Summary and are not
// required to match ExitCode — that mismatch is deliberate: it is how
// fixtures such as lie.yaml let tests prove a self-reported Summary can lie
// about the real outcome.
type Script struct {
	Patches   []Patch `yaml:"patches"`
	SleepMS   int     `yaml:"sleep_ms"`
	ExitCode  int     `yaml:"exit_code"`
	Claimed   string  `yaml:"claimed"`
	ExitNotes string  `yaml:"exit_notes"`
}

// LoadScript reads and parses a fake_script.yaml file from path.
func LoadScript(path string) (Script, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Script{}, fmt.Errorf("fake: read script %s: %w", path, err)
	}
	var s Script
	if err := yaml.Unmarshal(b, &s); err != nil {
		return Script{}, fmt.Errorf("fake: parse script %s: %w", path, err)
	}
	return s, nil
}
