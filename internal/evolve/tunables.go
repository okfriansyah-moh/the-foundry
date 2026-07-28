package evolve

import (
	"fmt"
	"os"

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
	var registry TunableRegistry
	if err := yaml.Unmarshal(raw, &registry); err != nil {
		return TunableRegistry{}, fmt.Errorf("evolve: parse tunables %s: %w", path, err)
	}
	return registry, nil
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
