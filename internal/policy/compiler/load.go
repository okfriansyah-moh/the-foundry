package compiler

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// docs/PLAN.md Task 116 (SEC-02): loaders for the org, profile and workflow
// policy layers. Before this, cmd/foundryd compiled platform-only (three empty
// layers), so the daemon's effective policy was platform-only — the org layer's
// tighter ceilings and its kernel-only push rule were never in force. These
// loaders read the real layer sources with STRICT schema validation (an unknown
// key rejects, so a field with a policy meaning cannot be silently dropped) and
// return a LayerPolicy plus, for the org layer, the OrgGovernancePack.

// layerYAML mirrors a layer source's policy fields. KnownFields(true) at decode
// time makes any key not listed here a hard error — a profile/org field with a
// policy meaning that this struct does not map is a load failure, not a silent
// drop (the fail-open this card closes).
type layerYAML struct {
	PermissionsAllowlist   []string                   `yaml:"permissions_allowlist"`
	DeploymentModes        map[string]Mode            `yaml:"deployment_modes"`
	BudgetCeilingsUSD      map[string]float64         `yaml:"budget_ceilings_usd"`
	ExecutorAllowlist      []string                   `yaml:"executor_allowlist"`
	ValidationAllowlistRef *string                    `yaml:"validation_allowlist_ref"`
	NotificationClasses    []string                   `yaml:"notification_classes"`
	RiskTierControls       map[string]RiskTierControl `yaml:"risk_tier_controls"`
	RequireSandbox         *bool                      `yaml:"require_sandbox"`
	// OrgGovernance is the org-layer-only governance extension (Task 54). It is
	// ignored for non-org layers (a profile/workflow that sets it is a load
	// error via KnownFields on the layer-specific decoders below).
	OrgGovernance *OrgGovernancePack `yaml:"org_governance"`
}

func (l layerYAML) toLayerPolicy() LayerPolicy {
	return LayerPolicy{
		PermissionsAllowlist:   l.PermissionsAllowlist,
		DeploymentModes:        l.DeploymentModes,
		BudgetCeilingsUSD:      l.BudgetCeilingsUSD,
		ExecutorAllowlist:      l.ExecutorAllowlist,
		ValidationAllowlistRef: l.ValidationAllowlistRef,
		NotificationClasses:    l.NotificationClasses,
		RiskTierControls:       l.RiskTierControls,
		RequireSandbox:         l.RequireSandbox,
	}
}

// decodeLayer strictly decodes raw into a layerYAML, rejecting unknown keys.
func decodeLayer(raw []byte, path string) (layerYAML, error) {
	var ly layerYAML
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&ly); err != nil {
		return layerYAML{}, fmt.Errorf("policy compiler: parse layer %s: %w", path, err)
	}
	return ly, nil
}

// LoadOrgLayer loads the organization policy layer plus its OrgGovernancePack
// from an org profile YAML (e.g. config/profiles/organization-10x.yaml). The
// governance pack is validated (only "kernel-only" push authorization is
// accepted). A nil governance stanza yields a zero pack, which denies all pushes
// (fail-closed).
func LoadOrgLayer(path string) (LayerPolicy, OrgGovernancePack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LayerPolicy{}, OrgGovernancePack{}, fmt.Errorf("policy compiler: read org layer %s: %w", path, err)
	}
	ly, err := decodeLayer(raw, path)
	if err != nil {
		return LayerPolicy{}, OrgGovernancePack{}, err
	}
	var pack OrgGovernancePack
	if ly.OrgGovernance != nil {
		pack = *ly.OrgGovernance
	}
	if err := ValidateOrgGovernancePack(pack); err != nil {
		return LayerPolicy{}, OrgGovernancePack{}, fmt.Errorf("policy compiler: org layer %s: %w", path, err)
	}
	return ly.toLayerPolicy(), pack, nil
}

// LoadProfileLayer loads the profile policy layer from a profile YAML (e.g.
// config/profiles/personal-autonomous-venture.yaml). A profile layer must not
// carry an org_governance stanza — that is org-layer-only.
func LoadProfileLayer(path string) (LayerPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LayerPolicy{}, fmt.Errorf("policy compiler: read profile layer %s: %w", path, err)
	}
	ly, err := decodeLayer(raw, path)
	if err != nil {
		return LayerPolicy{}, err
	}
	if ly.OrgGovernance != nil {
		return LayerPolicy{}, fmt.Errorf("policy compiler: profile layer %s must not declare org_governance (org-layer-only)", path)
	}
	return ly.toLayerPolicy(), nil
}

// WorkflowLayer returns the workflow policy layer. It defaults to empty
// EXPLICITLY (an empty workflow layer tightens nothing, which is correct — the
// fail-open was never at this layer; it was at org and profile). A future task
// deriving a workflow layer from a mission/workflow definition plugs in here.
func WorkflowLayer() LayerPolicy {
	return LayerPolicy{}
}

// CompileFourLayer loads and folds all four layers (platform → org → profile →
// workflow) and returns the Resolved policy plus the org governance pack. It is
// the single call cmd/foundryd uses instead of compiling platform-only.
// Empty orgPath or profilePath skips that layer (an empty layer tightens
// nothing) — but a NON-empty path that fails to load is a hard error, never a
// silent skip.
func CompileFourLayer(orgPath, profilePath string) (*Resolved, OrgGovernancePack, error) {
	platform, err := PlatformDefaults()
	if err != nil {
		return nil, OrgGovernancePack{}, err
	}
	var org LayerPolicy
	var pack OrgGovernancePack
	if orgPath != "" {
		if org, pack, err = LoadOrgLayer(orgPath); err != nil {
			return nil, OrgGovernancePack{}, err
		}
	}
	var profile LayerPolicy
	if profilePath != "" {
		if profile, err = LoadProfileLayer(profilePath); err != nil {
			return nil, OrgGovernancePack{}, err
		}
	}
	resolved, err := Compile(platform, org, profile, WorkflowLayer())
	if err != nil {
		return nil, OrgGovernancePack{}, err
	}
	return resolved, pack, nil
}
