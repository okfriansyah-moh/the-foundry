package compiler

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

// The canonical, ops-facing copy of this file is config/policy/
// platform.yaml. go:embed cannot traverse ".." out of this package
// directory (same constraint internal/profile/schema.go documents for
// Task 21's schema file), so embedded/platform.yaml is a build-time copy;
// platform_drift_test.go asserts byte-identity with the canonical copy so
// the two can never silently diverge.
//
//go:embed embedded/platform.yaml
var platformFS embed.FS

// platformYAML mirrors config/policy/platform.yaml's shape for
// unmarshaling.
type platformYAML struct {
	PermissionsAllowlist   []string                   `yaml:"permissions_allowlist"`
	DeploymentModes        map[string]Mode            `yaml:"deployment_modes"`
	BudgetCeilingsUSD      map[string]float64         `yaml:"budget_ceilings_usd"`
	ExecutorAllowlist      []string                   `yaml:"executor_allowlist"`
	ValidationAllowlistRef string                     `yaml:"validation_allowlist_ref"`
	NotificationClasses    []string                   `yaml:"notification_classes"`
	RiskTierControls       map[string]RiskTierControl `yaml:"risk_tier_controls"`
}

// PlatformDefaults loads and parses the embedded platform.yaml into a
// LayerPolicy suitable for Compile's platform argument.
func PlatformDefaults() (LayerPolicy, error) {
	raw, err := platformFS.ReadFile("embedded/platform.yaml")
	if err != nil {
		return LayerPolicy{}, fmt.Errorf("policy compiler: read embedded platform.yaml: %w", err)
	}
	var py platformYAML
	if err := yaml.Unmarshal(raw, &py); err != nil {
		return LayerPolicy{}, fmt.Errorf("policy compiler: parse embedded platform.yaml: %w", err)
	}
	ref := py.ValidationAllowlistRef
	return LayerPolicy{
		PermissionsAllowlist:   py.PermissionsAllowlist,
		DeploymentModes:        py.DeploymentModes,
		BudgetCeilingsUSD:      py.BudgetCeilingsUSD,
		ExecutorAllowlist:      py.ExecutorAllowlist,
		ValidationAllowlistRef: &ref,
		NotificationClasses:    py.NotificationClasses,
		RiskTierControls:       py.RiskTierControls,
	}, nil
}
