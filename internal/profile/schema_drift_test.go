package profile

import (
	"os"
	"testing"
)

// TestEmbeddedSchemaMatchesCanonicalCopy guards against internal/profile/
// schema/profile.schema.json (embedded, go:embed cannot cross "..") drifting
// from config/schemas/profile.schema.json (the canonical, ops-facing copy
// docs/PLAN.md Task 21 Outputs names). If this fails, copy one over the
// other — never edit only one.
func TestEmbeddedSchemaMatchesCanonicalCopy(t *testing.T) {
	embedded, err := schemaFS.ReadFile("schema/profile.schema.json")
	if err != nil {
		t.Fatalf("read embedded schema: %v", err)
	}
	canonical, err := os.ReadFile("../../config/schemas/profile.schema.json")
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if string(embedded) != string(canonical) {
		t.Fatalf("internal/profile/schema/profile.schema.json has drifted from config/schemas/profile.schema.json")
	}
}
