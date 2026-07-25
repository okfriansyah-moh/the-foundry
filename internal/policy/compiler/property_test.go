package compiler

import (
	"math/rand"
	"testing"
)

// shuffledPlatform returns a platform LayerPolicy that is
// internally-consistent with basePlatform (same sets, same map contents)
// but whose slices are permuted and whose maps are rebuilt by inserting
// keys in a randomized order — a different Go representation of the same
// configuration.
func shuffledPlatform(r *rand.Rand) LayerPolicy {
	perms := func(vs []string) []string {
		out := append([]string(nil), vs...)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}

	base := basePlatform()

	envKeys := perms([]string{"preview", "staging", "production"})
	modes := make(map[string]Mode, 3)
	for _, k := range envKeys {
		modes[k] = base.DeploymentModes[k]
	}

	budgetKeys := perms([]string{"workflow_usd", "task_usd"})
	budgets := make(map[string]float64, 2)
	for _, k := range budgetKeys {
		budgets[k] = base.BudgetCeilingsUSD[k]
	}

	tierKeys := perms([]string{"A0", "A1", "A2", "H"})
	tiers := make(map[string]RiskTierControl, 4)
	for _, k := range tierKeys {
		tiers[k] = base.RiskTierControls[k]
	}

	ref := *base.ValidationAllowlistRef
	return LayerPolicy{
		PermissionsAllowlist:   perms(base.PermissionsAllowlist),
		DeploymentModes:        modes,
		BudgetCeilingsUSD:      budgets,
		ExecutorAllowlist:      perms(base.ExecutorAllowlist),
		ValidationAllowlistRef: &ref,
		NotificationClasses:    perms(base.NotificationClasses),
		RiskTierControls:       tiers,
	}
}

// TestPropertyDigestIsOrderStable proves Step (5)'s property: the same
// configuration, represented with differently-ordered slices and maps
// built via differently-ordered insertions, always compiles to a
// byte-identical digest.
func TestPropertyDigestIsOrderStable(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	baseline, err := Compile(basePlatform(), LayerPolicy{}, LayerPolicy{}, LayerPolicy{})
	if err != nil {
		t.Fatalf("baseline compile: %v", err)
	}

	for i := 0; i < 50; i++ {
		resolved, err := Compile(shuffledPlatform(r), LayerPolicy{}, LayerPolicy{}, LayerPolicy{})
		if err != nil {
			t.Fatalf("iteration %d: compile: %v", i, err)
		}
		if resolved.Digest != baseline.Digest {
			t.Fatalf("iteration %d: digest = %q, want %q (baseline)", i, resolved.Digest, baseline.Digest)
		}
	}
}

// TestPropertyDigestIsDeterministicAcrossRepeatedCompiles proves the same
// exact inputs, compiled repeatedly, always produce the same digest (no
// hidden nondeterminism from map iteration inside a single Compile call).
func TestPropertyDigestIsDeterministicAcrossRepeatedCompiles(t *testing.T) {
	platform := basePlatform()
	org := LayerPolicy{PermissionsAllowlist: []string{"repo-write", "repo-read"}}

	var first string
	for i := 0; i < 20; i++ {
		resolved, err := Compile(platform, org, LayerPolicy{}, LayerPolicy{})
		if err != nil {
			t.Fatalf("iteration %d: compile: %v", i, err)
		}
		if i == 0 {
			first = resolved.Digest
			continue
		}
		if resolved.Digest != first {
			t.Fatalf("iteration %d: digest = %q, want %q", i, resolved.Digest, first)
		}
	}
}
