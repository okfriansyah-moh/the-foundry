package mission

import (
	"bytes"
	"embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// The canonical, ops-facing copy of this schema is config/schemas/
// mission.schema.json (docs/PLAN.md Task 40 Outputs). go:embed cannot
// traverse ".." out of this package directory, so schema/mission.schema.json
// is a build-time copy embedded for validation; schema_drift_test.go
// asserts byte-identity with the canonical copy (mirrors
// internal/profile/schema.go's own embedding pattern) so the two can never
// silently diverge.
//
//go:embed schema/mission.schema.json
var schemaFS embed.FS

const missionSchemaID = "https://foundry.dev/schemas/mission-contract/v1.json"

var missionSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	raw, err := schemaFS.ReadFile("schema/mission.schema.json")
	if err != nil {
		panic(fmt.Sprintf("mission: read embedded schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(missionSchemaID, bytes.NewReader(raw)); err != nil {
		panic(fmt.Sprintf("mission: add schema resource: %v", err))
	}
	schema, err := c.Compile(missionSchemaID)
	if err != nil {
		panic(fmt.Sprintf("mission: compile schema: %v", err))
	}
	return schema
}

// validateDocument validates generic (a parsed-YAML value, normalized via
// normalizeYAML/toJSONInstance) against config/schemas/mission.schema.json.
// On failure it returns the most specific *ContractError found (deepest
// schema-validation cause), pointing at the JSON path that actually
// violated the schema -- mirrors internal/profile.ValidateConfig.
func validateDocument(generic interface{}) error {
	instance, err := toJSONInstance(generic)
	if err != nil {
		return err
	}
	if err := missionSchema.Validate(instance); err != nil {
		ve, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return &ContractError{Path: "", Message: err.Error()}
		}
		leaf := deepestCause(ve)
		return &ContractError{Path: leaf.InstanceLocation, Message: leaf.Message}
	}
	return nil
}

// deepestCause walks a jsonschema.ValidationError's Causes tree to the
// first leaf (a node with no further causes), which is the most specific
// violation rather than the generic "doesn't validate against schema"
// wrapper at the root.
func deepestCause(ve *jsonschema.ValidationError) *jsonschema.ValidationError {
	for len(ve.Causes) > 0 {
		ve = ve.Causes[0]
	}
	return ve
}
