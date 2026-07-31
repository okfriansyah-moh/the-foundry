package compiler

import (
	"encoding/json"
	"fmt"
)

// docs/PLAN.md Task 116 (SEC-02): a profile's recorded policy_digest must be the
// digest of its COMPILED policy, not sha256 of its raw config bytes. A config
// hash is meaningless to anything downstream that wants to trust the effective
// policy; the compiled digest binds the profile to the exact platform+profile
// fold that will actually be enforced.

// profileConfigView is the policy-meaningful slice of a profile config. Every
// field here maps into a LayerPolicy; a profile config field with a policy
// meaning that is NOT mapped here is a gap this view must grow to cover, rather
// than be silently dropped (the fail-open this card closes).
type profileConfigView struct {
	Budget struct {
		MaxUSD float64 `json:"max_usd"`
	} `json:"budget"`
}

// ProfileLayerFromConfig maps a profile's config into its policy LayerPolicy.
// It is the single source of truth for that mapping, shared by the CLI
// (`foundry policy resolve`, `foundry profile create`) and the API
// (POST /v1/profiles), so the two can never drift.
func ProfileLayerFromConfig(raw json.RawMessage) (LayerPolicy, error) {
	var cfg profileConfigView
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return LayerPolicy{}, fmt.Errorf("compiler: parse profile config: %w", err)
	}
	return LayerPolicy{
		BudgetCeilingsUSD: map[string]float64{"workflow_usd": cfg.Budget.MaxUSD},
	}, nil
}

// ProfilePolicyDigest compiles platform + the profile layer derived from config
// and returns the resolved policy digest. This is the value a profile's
// policy_digest must carry (Task 116), replacing the sha256-of-config
// placeholder.
func ProfilePolicyDigest(raw json.RawMessage) (string, error) {
	platform, err := PlatformDefaults()
	if err != nil {
		return "", fmt.Errorf("compiler: platform defaults: %w", err)
	}
	profileLayer, err := ProfileLayerFromConfig(raw)
	if err != nil {
		return "", err
	}
	resolved, err := Compile(platform, LayerPolicy{}, profileLayer, LayerPolicy{})
	if err != nil {
		return "", fmt.Errorf("compiler: compile profile policy: %w", err)
	}
	return resolved.Digest, nil
}
