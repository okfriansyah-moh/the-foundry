package claudecode

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/contracttest"
)

// TestContractLeak proves the shipped claude-code adapter (Task 17) honors
// the fresh-context-per-invocation contract (docs/PLAN.md Task 91 / PRV-08,
// authority-model.md N7.5) — no mutable state crosses two instances.
func TestContractLeak(t *testing.T) {
	contracttest.LeakCheck(t, contracttest.Options{
		Name: "claude-code",
		New:  func() executor.Adapter { return New() },
	})
}
