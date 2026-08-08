package evolve

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Tunable struct {
	Name   string  `yaml:"name"`
	Min    float64 `yaml:"min"`
	Max    float64 `yaml:"max"`
	Scope  string  `yaml:"scope"`
	Metric string  `yaml:"metric"`
}

type TunableRegistry struct {
	Tunables []Tunable `yaml:"tunables"`
}

func LoadTunables(path string) (TunableRegistry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TunableRegistry{}, fmt.Errorf("evolve: read tunables %s: %w", path, err)
	}
	return ParseTunablesYAML(raw, path)
}

// ParseTunablesYAML decodes tunables registry YAML bytes.
func ParseTunablesYAML(raw []byte, source string) (TunableRegistry, error) {
	if strings.TrimSpace(source) == "" {
		source = "<memory>"
	}
	var registry TunableRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return TunableRegistry{}, fmt.Errorf("evolve: parse tunables %s: %w", source, err)
	}
	return registry, nil
}

// ParseTunableValuesYAML decodes arbitrary YAML bytes into out for value-only documents.
func ParseTunableValuesYAML(raw []byte, source string, out any) error {
	if strings.TrimSpace(source) == "" {
		source = "<memory>"
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("evolve: parse tunable values %s: %w", source, err)
	}
	return nil
}

func (r TunableRegistry) Lookup(name string) (Tunable, bool) {
	for _, tunable := range r.Tunables {
		if tunable.Name == name {
			return tunable, true
		}
	}
	return Tunable{}, false
}

func (r TunableRegistry) InBounds(name string, value float64) bool {
	tunable, ok := r.Lookup(name)
	if !ok {
		return false
	}
	return value >= tunable.Min && value <= tunable.Max
}
