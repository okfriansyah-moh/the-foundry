package detect

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

func TestDetect_PositiveAndNegative(t *testing.T) {
	doc := &plan.Document{
		Tasks: []plan.Task{
			{
				ID: "t1",
				Files: []string{
					"go.mod",
					"migrations/001.sql",
					"internal/billing/stripe.go",
					"deploy/fly.toml",
					"internal/secrets/.env.example",
					"internal/authz/policy.yaml",
					"README.md",
				},
				Commands: []string{
					"curl https://api.stripe.com",
					"psql -c 'DROP TABLE old_data'",
				},
			},
		},
	}
	got := FromDocument(doc)
	if len(got) == 0 {
		t.Fatal("expected detected effects, got none")
	}
	assertHasKind(t, got, plan.EffectDependency)
	assertHasKind(t, got, plan.EffectMigration)
	assertHasKind(t, got, plan.EffectBilling)
	assertHasKind(t, got, plan.EffectDeploy)
	assertHasKind(t, got, plan.EffectSecret)
	assertHasKind(t, got, plan.EffectPermission)
	assertHasKind(t, got, plan.EffectNetwork)
	assertHasKind(t, got, plan.EffectDestructive)
}

func TestDetect_DeterministicAcrossRuns(t *testing.T) {
	doc := &plan.Document{
		Tasks: []plan.Task{{
			ID:       "t1",
			Files:    []string{"go.mod", "deploy/fly.toml", "internal/billing/stripe.go"},
			Commands: []string{"curl https://api.example.com"},
		}},
	}
	first := FromDocument(doc)
	for i := 0; i < 5; i++ {
		next := FromDocument(doc)
		if !sameEffects(first, next) {
			t.Fatalf("run %d differs from run 0", i+1)
		}
	}
}

func assertHasKind(t *testing.T, effects []plan.Effect, kind plan.EffectKind) {
	t.Helper()
	for _, e := range effects {
		if e.Kind == kind {
			return
		}
	}
	t.Fatalf("expected effect kind %s not found: %+v", kind, effects)
}

func sameEffects(a, b []plan.Effect) bool {
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
