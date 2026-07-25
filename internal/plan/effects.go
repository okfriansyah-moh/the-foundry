package plan

// EffectKind enumerates the declarable categories of side effect a plan task
// may claim it will perform. This is the closed set from docs/PLAN.md Task 6;
// any other value is a strict-mode parse error.
type EffectKind string

// Declared effect kinds. Order matches docs/PLAN.md Task 6's enum listing.
const (
	EffectDocs        EffectKind = "docs"
	EffectCode        EffectKind = "code"
	EffectDependency  EffectKind = "dependency"
	EffectMigration   EffectKind = "migration"
	EffectBilling     EffectKind = "billing"
	EffectSecret      EffectKind = "secret"
	EffectNetwork     EffectKind = "network"
	EffectDeploy      EffectKind = "deploy"
	EffectPermission  EffectKind = "permission"
	EffectDestructive EffectKind = "destructive"
)

var validEffectKinds = map[EffectKind]struct{}{
	EffectDocs:        {},
	EffectCode:        {},
	EffectDependency:  {},
	EffectMigration:   {},
	EffectBilling:     {},
	EffectSecret:      {},
	EffectNetwork:     {},
	EffectDeploy:      {},
	EffectPermission:  {},
	EffectDestructive: {},
}

// Valid reports whether k is one of the closed set of declarable effect
// kinds.
func (k EffectKind) Valid() bool {
	_, ok := validEffectKinds[k]
	return ok
}

// Effect is a single declared side effect a plan claims its tasks will
// produce. Declaration is advisory: admission (Task 7) compares Declared
// against independently Detected effects.
type Effect struct {
	Kind   EffectKind `yaml:"kind"`
	Target string     `yaml:"target"`
}

// Permission is a single permission a plan requests. Requesting a
// permission never grants it — granted permissions are always the
// policy-validated subset, decided outside this package
// (docs/foundry/docs/security/approval-and-provenance.md §2).
type Permission struct {
	Kind   string `yaml:"kind"`
	Target string `yaml:"target"`
}
