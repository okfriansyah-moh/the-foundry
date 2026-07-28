package kernel

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadRoutingTable reads and strictly validates config/executor-routing.yaml
// (docs/PLAN.md Task 90 / PRV-07) into a RoutingTable. Unknown top-level keys
// are rejected (KnownFields). The result is consumed by ExecutorSelector as
// its default-selection source for classed tasks that name no explicit
// executor. An empty/absent file yields an empty table (routing inactive),
// which is a valid state — the selector then falls back to its Default.
func LoadRoutingTable(path string) (RoutingTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kernel: read routing table %s: %w", path, err)
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
