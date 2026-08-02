package mission

import "testing"

// TestImprovementLoop_SymbolExists keeps the replay/register surface compile-checked
// (docs/PLAN.md Task 147).
func TestImprovementLoop_SymbolExists(t *testing.T) {
	_ = ImprovementLoop
}
