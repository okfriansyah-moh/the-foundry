package cost

import (
	"fmt"
	"math"
	"os"

	"gopkg.in/yaml.v3"
)

// Defaults is config/cost-defaults.yaml's parsed shape: the fallback
// per-task cost estimate the kernel's pre-task reservation hook uses.
//
// decision (no-gaps rule): the task card allows a task's own "packet-
// declared estimate" to take precedence over this default, but
// internal/plan.Task (the executable-plan schema internal/plan owns, not
// this task) has no cost-estimate field today — adding one is a
// plan-schema change outside this task's Steps. The smallest reversible
// option is to use this single default for every task now; a later plan-
// schema task can add plan.Task.CostEstimateUSD and have ReserveBudget
// prefer it without changing behavior for plans that don't set it (the same
// pattern internal/kernel/workflow.go's defaultTaskTimeout decision already
// uses).
type Defaults struct {
	DefaultUSD float64 `yaml:"default_usd"`
}

// LoadDefaults reads and parses Defaults from a
// config/cost-defaults.yaml-shaped file.
func LoadDefaults(path string) (Defaults, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Defaults{}, fmt.Errorf("cost: read defaults %s: %w", path, err)
	}
	var d Defaults
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return Defaults{}, fmt.Errorf("cost: parse defaults %s: %w", path, err)
	}
	if d.DefaultUSD <= 0 || math.IsNaN(d.DefaultUSD) || math.IsInf(d.DefaultUSD, 0) {
		return Defaults{}, fmt.Errorf("cost: defaults %s: default_usd must be a positive finite number, got %v", path, d.DefaultUSD)
	}
	return d, nil
}
