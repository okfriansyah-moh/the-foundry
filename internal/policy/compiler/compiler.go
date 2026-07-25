package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Direction explains which way a layer moved a field.
type Direction string

const (
	DirectionTightened Direction = "tightened"
	DirectionChanged   Direction = "changed" // a RuleFree field was set/changed
)

// Override is one layer's recorded explanation for changing one field.
// Old/New are formatted with fmt.Sprintf("%v", ...) so every field type
// (slice, map, string) renders as a readable string without a type switch
// at every call site.
type Override struct {
	Field     string
	FromLayer Layer
	Old       string
	New       string
	Direction Direction
}

// Resolved is the compiler's output: the fully-merged Effective policy,
// every Override any non-platform layer made, and a digest of Effective
// that is stable across internally-consistent representations of the same
// inputs (Step (5)'s property test).
type Resolved struct {
	Effective Policy
	Overrides []Override
	Digest    string
}

// Compile folds platform -> org -> profile -> workflow, in that fixed
// order (docs/foundry/docs/architecture/configuration-and-policy.md
// N6.1). platform must set every field; org, profile, and workflow may
// each tighten (never weaken) a tighten-only field, must never change a
// fixed field, and may freely set a free field. The first violation
// found is returned as a *CompileError naming its layer and field — not a
// warning, not a silent override.
func Compile(platform, org, profile, workflow LayerPolicy) (*Resolved, error) {
	eff, err := platformEffective(platform)
	if err != nil {
		return nil, err
	}

	var overrides []Override
	for _, step := range []struct {
		layer Layer
		next  LayerPolicy
	}{
		{LayerOrg, org},
		{LayerProfile, profile},
		{LayerWorkflow, workflow},
	} {
		if err := applyLayer(&eff, step.layer, step.next, &overrides); err != nil {
			return nil, err
		}
	}

	digest, err := digestOf(eff)
	if err != nil {
		return nil, fmt.Errorf("policy compile: digest: %w", err)
	}

	return &Resolved{Effective: eff, Overrides: overrides, Digest: digest}, nil
}

// platformEffective builds the starting Effective policy from the
// platform layer, requiring every field to be set — the platform layer is
// the ceiling every lower layer can only tighten.
func platformEffective(p LayerPolicy) (Policy, error) {
	missing := func(field string) error {
		return &CompileError{Layer: LayerPlatform, Field: field, Message: "platform layer must set this field"}
	}
	if p.PermissionsAllowlist == nil {
		return Policy{}, missing("permissions_allowlist")
	}
	if len(p.DeploymentModes) == 0 {
		return Policy{}, missing("deployment_modes")
	}
	if len(p.BudgetCeilingsUSD) == 0 {
		return Policy{}, missing("budget_ceilings_usd")
	}
	if p.ExecutorAllowlist == nil {
		return Policy{}, missing("executor_allowlist")
	}
	if p.ValidationAllowlistRef == nil || *p.ValidationAllowlistRef == "" {
		return Policy{}, missing("validation_allowlist_ref")
	}
	if p.NotificationClasses == nil {
		return Policy{}, missing("notification_classes")
	}
	if len(p.RiskTierControls) == 0 {
		return Policy{}, missing("risk_tier_controls")
	}
	for env, mode := range p.DeploymentModes {
		if !mode.valid() {
			return Policy{}, &CompileError{Layer: LayerPlatform, Field: "deployment_modes", Message: fmt.Sprintf("env %q: invalid mode %q", env, mode)}
		}
	}

	return Policy{
		PermissionsAllowlist:   sortedCopy(p.PermissionsAllowlist),
		DeploymentModes:        copyMap(p.DeploymentModes),
		BudgetCeilingsUSD:      copyMap(p.BudgetCeilingsUSD),
		ExecutorAllowlist:      sortedCopy(p.ExecutorAllowlist),
		ValidationAllowlistRef: *p.ValidationAllowlistRef,
		NotificationClasses:    sortedCopy(p.NotificationClasses),
		RiskTierControls:       copyMap(p.RiskTierControls),
	}, nil
}

// applyLayer merges one non-platform layer into eff in place, appending
// any resulting Override.
func applyLayer(eff *Policy, layer Layer, next LayerPolicy, overrides *[]Override) error {
	if next.PermissionsAllowlist != nil {
		merged, ov, err := mergeStringSet("permissions_allowlist", layer, eff.PermissionsAllowlist, next.PermissionsAllowlist)
		if err != nil {
			return err
		}
		eff.PermissionsAllowlist = merged
		appendIfSet(overrides, ov)
	}
	if next.ExecutorAllowlist != nil {
		merged, ov, err := mergeStringSet("executor_allowlist", layer, eff.ExecutorAllowlist, next.ExecutorAllowlist)
		if err != nil {
			return err
		}
		eff.ExecutorAllowlist = merged
		appendIfSet(overrides, ov)
	}
	if next.NotificationClasses != nil {
		merged, ov, err := mergeStringSet("notification_classes", layer, eff.NotificationClasses, next.NotificationClasses)
		if err != nil {
			return err
		}
		eff.NotificationClasses = merged
		appendIfSet(overrides, ov)
	}
	if len(next.DeploymentModes) > 0 {
		ovs, err := mergeDeploymentModes(layer, eff.DeploymentModes, next.DeploymentModes)
		if err != nil {
			return err
		}
		*overrides = append(*overrides, ovs...)
	}
	if len(next.BudgetCeilingsUSD) > 0 {
		ovs, err := mergeBudgets(layer, eff.BudgetCeilingsUSD, next.BudgetCeilingsUSD)
		if err != nil {
			return err
		}
		*overrides = append(*overrides, ovs...)
	}
	if len(next.RiskTierControls) > 0 {
		ovs, err := mergeRiskTiers(layer, eff.RiskTierControls, next.RiskTierControls)
		if err != nil {
			return err
		}
		*overrides = append(*overrides, ovs...)
	}
	if next.ValidationAllowlistRef != nil {
		ov, err := mergeFixedString("validation_allowlist_ref", layer, eff.ValidationAllowlistRef, *next.ValidationAllowlistRef)
		if err != nil {
			return err
		}
		appendIfSet(overrides, ov)
	}
	return nil
}

func appendIfSet(overrides *[]Override, ov *Override) {
	if ov != nil {
		*overrides = append(*overrides, *ov)
	}
}

// mergeStringSet merges a tighten-only string-set field: next must be a
// subset of prev (every element in next already present in prev), or the
// layer widened the set — a compile error naming the offending elements.
func mergeStringSet(field string, layer Layer, prev, next []string) ([]string, *Override, error) {
	prevSet := toSet(prev)
	nextSorted := sortedCopy(next)
	var added []string
	for _, v := range nextSorted {
		if !prevSet[v] {
			added = append(added, v)
		}
	}
	if len(added) > 0 {
		return nil, nil, &CompileError{
			Layer:   layer,
			Field:   field,
			Message: fmt.Sprintf("attempted to widen %s beyond the inherited set %v by adding %v", field, prev, added),
		}
	}
	if equalStrings(prev, nextSorted) {
		return prev, nil, nil
	}
	return nextSorted, &Override{
		Field:     field,
		FromLayer: layer,
		Old:       fmt.Sprintf("%v", prev),
		New:       fmt.Sprintf("%v", nextSorted),
		Direction: DirectionTightened,
	}, nil
}

// mergeDeploymentModes merges tighten-only per-environment deployment
// modes: an env key must already exist in prev (introduced only by
// platform); a lower layer may only move an env's mode to an equal or
// higher restrictiveness rank.
func mergeDeploymentModes(layer Layer, prev map[string]Mode, next map[string]Mode) ([]Override, error) {
	var overrides []Override
	for _, env := range sortedMapKeys(next) {
		newMode := next[env]
		if !newMode.valid() {
			return nil, &CompileError{Layer: layer, Field: "deployment_modes", Message: fmt.Sprintf("env %q: invalid mode %q", env, newMode)}
		}
		oldMode, ok := prev[env]
		if !ok {
			return nil, &CompileError{Layer: layer, Field: "deployment_modes", Message: fmt.Sprintf("env %q is not defined by the platform layer", env)}
		}
		if modeRank[newMode] < modeRank[oldMode] {
			return nil, &CompileError{
				Layer:   layer,
				Field:   "deployment_modes",
				Message: fmt.Sprintf("env %q: attempted to weaken mode %q -> %q", env, oldMode, newMode),
			}
		}
		if newMode == oldMode {
			continue
		}
		prev[env] = newMode
		overrides = append(overrides, Override{
			Field:     "deployment_modes",
			FromLayer: layer,
			Old:       fmt.Sprintf("%s=%s", env, oldMode),
			New:       fmt.Sprintf("%s=%s", env, newMode),
			Direction: DirectionTightened,
		})
	}
	return overrides, nil
}

// mergeBudgets merges tighten-only budget ceilings: a budget key must
// already exist in prev; a lower layer may only lower the ceiling, never
// raise it.
func mergeBudgets(layer Layer, prev map[string]float64, next map[string]float64) ([]Override, error) {
	var overrides []Override
	for _, name := range sortedMapKeys(next) {
		newCeiling := next[name]
		oldCeiling, ok := prev[name]
		if !ok {
			return nil, &CompileError{Layer: layer, Field: "budget_ceilings_usd", Message: fmt.Sprintf("budget %q is not defined by the platform layer", name)}
		}
		if newCeiling > oldCeiling {
			return nil, &CompileError{
				Layer:   layer,
				Field:   "budget_ceilings_usd",
				Message: fmt.Sprintf("budget %q: attempted to raise ceiling %.2f -> %.2f", name, oldCeiling, newCeiling),
			}
		}
		if newCeiling == oldCeiling {
			continue
		}
		prev[name] = newCeiling
		overrides = append(overrides, Override{
			Field:     "budget_ceilings_usd",
			FromLayer: layer,
			Old:       fmt.Sprintf("%s=%.2f", name, oldCeiling),
			New:       fmt.Sprintf("%s=%.2f", name, newCeiling),
			Direction: DirectionTightened,
		})
	}
	return overrides, nil
}

// mergeRiskTiers merges tighten-only per-tier risk controls: a tier key
// must already exist in prev; a lower layer may only make a tier's
// control at least as restrictive (RiskTierControl.stricterOrEqual).
func mergeRiskTiers(layer Layer, prev map[string]RiskTierControl, next map[string]RiskTierControl) ([]Override, error) {
	var overrides []Override
	for _, tier := range sortedMapKeys(next) {
		newCtl := next[tier]
		oldCtl, ok := prev[tier]
		if !ok {
			return nil, &CompileError{Layer: layer, Field: "risk_tier_controls", Message: fmt.Sprintf("tier %q is not defined by the platform layer", tier)}
		}
		if !oldCtl.stricterOrEqual(newCtl) {
			return nil, &CompileError{
				Layer:   layer,
				Field:   "risk_tier_controls",
				Message: fmt.Sprintf("tier %q: attempted to weaken %+v -> %+v", tier, oldCtl, newCtl),
			}
		}
		if newCtl == oldCtl {
			continue
		}
		prev[tier] = newCtl
		overrides = append(overrides, Override{
			Field:     "risk_tier_controls",
			FromLayer: layer,
			Old:       fmt.Sprintf("%s=%+v", tier, oldCtl),
			New:       fmt.Sprintf("%s=%+v", tier, newCtl),
			Direction: DirectionTightened,
		})
	}
	return overrides, nil
}

// mergeFixedString enforces RuleFixed: any layer supplying a value
// different from prev is a compile error, regardless of whether the new
// value would be "tighter" by some other measure.
func mergeFixedString(field string, layer Layer, prev string, next string) (*Override, error) {
	if next == prev {
		return nil, nil
	}
	return nil, &CompileError{
		Layer:   layer,
		Field:   field,
		Message: fmt.Sprintf("%s is fixed by the platform layer (%q); layers may not change it (attempted %q)", field, prev, next),
	}
}

// digestOf produces a sha256 digest of eff. encoding/json sorts map[string]
// keys and eff's slice fields are always stored pre-sorted (sortedCopy),
// so the marshaled bytes — and therefore the digest — are stable
// regardless of insertion order in any of the layer inputs that produced
// eff (Step (5)'s determinism/order-stability property).
func digestOf(eff Policy) (string, error) {
	raw, err := json.Marshal(eff)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func toSet(vs []string) map[string]bool {
	out := make(map[string]bool, len(vs))
	for _, v := range vs {
		out[v] = true
	}
	return out
}

func sortedCopy(vs []string) []string {
	out := append([]string(nil), vs...)
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyMap returns a shallow copy of m; used to give the effective policy
// its own map per field instead of aliasing a layer input's map.
func copyMap[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// sortedMapKeys returns m's keys sorted, so every map field is walked in a
// fixed order — no map-iteration-order leaking into Overrides ordering or
// the digest.
func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
