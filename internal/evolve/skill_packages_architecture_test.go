package evolve

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSkillPackageBridgeHasNoSCMOrDeployAuthority keeps the Task 155 bridge
// bounded to canonical package state. SCM writes, deploy decisions, executor
// materialization, and PEC decisions remain separate authority boundaries.
func TestSkillPackageBridgeHasNoSCMOrDeployAuthority(t *testing.T) {
	paths, err := filepath.Glob("skill_package*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"/internal/scm/write",
			"/internal/deploy",
			"/internal/pec",
			"/adapters/agent-runtime",
		} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s imports forbidden authority surface %q", path, forbidden)
			}
		}
	}
}
