package apiexec

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelPolicy is the config-driven "model per task class" routing hook
// (docs/PLAN.md Task 79 / EVO-06): per provider, a map of task class → model,
// with a "default" fallback. The API adapter (apiexec.Adapter.Prepare)
// resolves the model for the incoming packet's Class against this policy and
// uses a class-specific hit for that request, so routing can pick a
// cheaper/faster model per class without a code change. This is distinct from
// Config.ModelEnv, which is only a blunt per-provider override.
type ModelPolicy struct {
	// Models maps provider name → (task class → model). The class key
	// "default" is the fallback when a specific class is absent.
	Models map[string]map[string]string `yaml:"models"`
}

// LoadModelPolicy reads and strictly validates a model-policy YAML file. A
// MISSING file is non-fatal (returns an empty policy and nil error — per-class
// routing is simply disabled). A present-but-invalid file (malformed YAML or
// an unknown key rejected by KnownFields) returns an error, so a
// misconfiguration fails loudly rather than silently reverting every task to
// the default model.
func LoadModelPolicy(path string) (ModelPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ModelPolicy{}, nil
		}
		return ModelPolicy{}, fmt.Errorf("apiexec: read model policy %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var p ModelPolicy
	if err := dec.Decode(&p); err != nil {
		return ModelPolicy{}, fmt.Errorf("apiexec: parse model policy %s: %w", path, err)
	}
	return p, nil
}

// Resolve returns the model for provider and taskClass: the class-specific
// model if configured, else the provider's "default", else "" (meaning "use
// the adapter's own built-in default"). Deterministic and pure.
func (p ModelPolicy) Resolve(provider, taskClass string) string {
	byClass, ok := p.Models[provider]
	if !ok {
		return ""
	}
	if taskClass != "" {
		if m, ok := byClass[taskClass]; ok {
			return m
		}
	}
	return byClass["default"]
}
