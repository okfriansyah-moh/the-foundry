package mockup

import (
	"os"
	"testing"
)

// Seeded doclint violation for docs/PLAN.md Task 131 (DOC-01): tests must
// not write into the package source tree.
func TestWritesIntoPackageSource(t *testing.T) {
	_ = os.WriteFile("leaked-fixture.txt", []byte("x"), 0o644)
}
