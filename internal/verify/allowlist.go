package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// allowlistFile is the on-disk shape of config/validation-allowlist.yaml.
type allowlistFile struct {
	Commands   []string `yaml:"commands"`
	ScriptsDir string   `yaml:"scripts_dir"`
}

// Allowlist is the policy Runner checks every command's first token
// against before executing anything (docs/PLAN.md Task 13 Step 2). It is
// the same "policy stub" role internal/provenance.AllowList plays for
// permissions — a stand-in for a future real policy store, not a network
// object.
type Allowlist struct {
	commands   map[string]bool
	scriptsDir string
}

// LoadAllowlist reads and parses an Allowlist from a
// config/validation-allowlist.yaml-shaped file.
func LoadAllowlist(path string) (Allowlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Allowlist{}, fmt.Errorf("verify: read allowlist %s: %w", path, err)
	}
	var f allowlistFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return Allowlist{}, fmt.Errorf("verify: parse allowlist %s: %w", path, err)
	}
	set := make(map[string]bool, len(f.Commands))
	for _, c := range f.Commands {
		set[c] = true
	}
	return Allowlist{commands: set, scriptsDir: f.ScriptsDir}, nil
}

// Check reports whether argv (already tokenized, e.g. by shlexSplit) may
// run at all: either argv[0] is a plain allowlisted command, or argv[0] is
// "bash" and argv[1] names a script path under the allowlist's
// scripts_dir. Anything else — including a bare "bash -c ...", an
// unlisted binary, or an empty argv — is refused.
func (a Allowlist) Check(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("verify: empty command")
	}
	head := argv[0]
	if a.commands[head] {
		return nil
	}
	if head == "bash" && len(argv) > 1 && a.isAllowedScript(argv[1]) {
		return nil
	}
	return fmt.Errorf("verify: %q is not in the validation allowlist", head)
}

// isAllowedScript reports whether scriptPath is a path under a.scriptsDir.
func (a Allowlist) isAllowedScript(scriptPath string) bool {
	if a.scriptsDir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(a.scriptsDir), filepath.Clean(scriptPath))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
