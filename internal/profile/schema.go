package profile

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// ConfigSchemaVersion is the schema_version value profiles.config must carry
// for this build of Foundry to accept it. A future config shape change ships
// as a new schema_version (and, if needed, a new schema file) — existing
// profiles saved under an older version are never silently reinterpreted
// under the new shape.
const ConfigSchemaVersion = 1

// The canonical, ops-facing copy of this schema is config/schemas/
// profile.schema.json (docs/PLAN.md Task 21 Outputs). go:embed cannot
// traverse ".." out of this package directory, so schema/profile.schema.json
// is a build-time copy embedded for validation; schema_drift_test.go asserts
// byte-identity with the canonical copy so the two can never silently
// diverge.
//
//go:embed schema/profile.schema.json
var schemaFS embed.FS

const schemaID = "https://foundry.dev/schemas/profile-config/v1.json"

var configSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	raw, err := schemaFS.ReadFile("schema/profile.schema.json")
	if err != nil {
		panic(fmt.Sprintf("profile: read embedded schema: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaID, bytes.NewReader(raw)); err != nil {
		panic(fmt.Sprintf("profile: add schema resource: %v", err))
	}
	schema, err := c.Compile(schemaID)
	if err != nil {
		panic(fmt.Sprintf("profile: compile schema: %v", err))
	}
	return schema
}

// ConfigError describes one JSONSchema violation in a profile config, with a
// JSON-pointer-style path to the offending field rooted at "/config" (e.g.
// "/config/budget/max_usd"), not a generic "invalid" message.
type ConfigError struct {
	Path    string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidateConfig parses raw as JSON and validates it against
// config/schemas/profile.schema.json (embedded at internal/profile/schema/
// profile.schema.json). On failure it returns the most specific *ConfigError
// found (deepest schema-validation cause), pointing at the JSON path that
// actually violated the schema.
func ValidateConfig(raw json.RawMessage) error {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return &ConfigError{Path: "/config", Message: fmt.Sprintf("not valid JSON: %v", err)}
	}
	// jsonschema requires float64/string/bool/map/slice/nil instances,
	// which encoding/json's default Unmarshal into interface{} already
	// produces.
	if err := configSchema.Validate(v); err != nil {
		ve, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return &ConfigError{Path: "/config", Message: err.Error()}
		}
		leaf := deepestCause(ve)
		return &ConfigError{
			Path:    "/config" + leaf.InstanceLocation,
			Message: leaf.Message,
		}
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
