package signals

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Allowlist is the closed, configurable set of evidence classes that may
// satisfy must_have_real_validation_signal. Unknown classes are refused.
type Allowlist struct {
	Classes map[Class]bool
}

// DefaultAllowlist is the V1 closed set from docs/PLAN.md Task 139.
func DefaultAllowlist() Allowlist {
	a := Allowlist{Classes: map[Class]bool{}}
	for _, c := range []Class{
		ClassLandingConversion,
		ClassWaitlistSignup,
		ClassPricingCTA,
		ClassQualifiedInbound,
		ClassTrafficExperiment,
		ClassInterviewProspect,
	} {
		a.Classes[c] = true
	}
	return a
}

// Contains reports whether c is allowlisted.
func (a Allowlist) Contains(c Class) bool {
	if a.Classes == nil {
		return false
	}
	return a.Classes[c]
}

// Names returns sorted class names for deterministic serialization.
func (a Allowlist) Names() []string {
	out := make([]string, 0, len(a.Classes))
	for c, ok := range a.Classes {
		if ok {
			out = append(out, string(c))
		}
	}
	sort.Strings(out)
	return out
}

type allowlistYAML struct {
	Classes []string `yaml:"classes"`
}

// LoadAllowlist reads a YAML allowlist. Empty/missing classes refuse all.
func LoadAllowlist(path string) (Allowlist, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Allowlist{}, fmt.Errorf("signals: read allowlist %s: %w", path, err)
	}
	var y allowlistYAML
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return Allowlist{}, fmt.Errorf("signals: parse allowlist %s: %w", path, err)
	}
	a := Allowlist{Classes: map[Class]bool{}}
	for _, name := range y.Classes {
		c := Class(name)
		if !c.Valid() {
			return Allowlist{}, fmt.Errorf("signals: unknown class %q in allowlist (closed vocabulary)", name)
		}
		a.Classes[c] = true
	}
	return a, nil
}
