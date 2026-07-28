package kernel

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadRoutingTable reads and strictly validates config/executor-routing.yaml
// (docs/PLAN.md Task 90 / PRV-07) into a RoutingTable. Unknown top-level keys
// are rejected (KnownFields). An ABSENT file yields an empty table (routing
// inactive) — a valid state where the selector falls back to its Default. A
// present-but-invalid file (malformed YAML, unknown key, empty preference
// list) returns an error, consistent with foundryd's fail-closed config
// loading convention.
func LoadRoutingTable(path string) (RoutingTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RoutingTable{}, nil
		}
		return nil, fmt.Errorf("kernel: read routing table %s: %w", path, err)
	}
	// An empty file is also valid — routing simply inactive.
	if len(bytes.TrimSpace(raw)) == 0 {
		return RoutingTable{}, nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var doc struct {
		Routes map[string][]string `yaml:"routes"`
	}
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("kernel: parse routing table %s: %w", path, err)
	}
	for class, prefs := range doc.Routes {
		if len(prefs) == 0 {
			return nil, fmt.Errorf("kernel: routing table %s: class %q has an empty preference list", path, class)
		}
	}
	return RoutingTable(doc.Routes), nil
}
