package retention

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type RetentionClass struct {
	Name          string   `yaml:"name"`
	TTL           string   `yaml:"ttl"`
	Cascade       []string `yaml:"cascade"`
	AccessLogging bool     `yaml:"access_logging"`
}

type RegistryFile struct {
	Classes []RetentionClass `yaml:"classes"`
}

type Registry map[string]RetentionClass

func LoadRegistry(path string) (Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("retention: read registry %s: %w", path, err)
	}
	var file RegistryFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("retention: parse registry %s: %w", path, err)
	}
	registry := make(Registry, len(file.Classes))
	for _, class := range file.Classes {
		if _, err := time.ParseDuration(class.TTL); err != nil {
			return nil, fmt.Errorf("retention: class %s ttl: %w", class.Name, err)
		}
		registry[class.Name] = class
	}
	return registry, nil
}

func (r Registry) TTL(name string) (time.Duration, error) {
	class, ok := r[name]
	if !ok {
		return 0, fmt.Errorf("retention: unknown class %q", name)
	}
	return time.ParseDuration(class.TTL)
}
