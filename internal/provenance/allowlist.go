package provenance

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

// allowListFile is the on-disk shape of config/permissions-allowlist.yaml.
type allowListFile struct {
	Permissions []plan.Permission `yaml:"permissions"`
}

// AllowList is the policy stub Granted is computed against (docs/PLAN.md
// Task 8 Step 1: "Granted = Requested ∩ policy stub allowlist"). It is a
// stand-in for the real policy store — the same stub role NoopPolicyView
// plays for admission (Task 7's out-of-scope note applies identically
// here).
type AllowList struct {
	entries []plan.Permission
}

// LoadAllowList reads and parses an AllowList from a
// config/permissions-allowlist.yaml-shaped file.
func LoadAllowList(path string) (AllowList, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AllowList{}, fmt.Errorf("provenance: read allowlist %s: %w", path, err)
	}
	var f allowListFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return AllowList{}, fmt.Errorf("provenance: parse allowlist %s: %w", path, err)
	}
	return AllowList{entries: f.Permissions}, nil
}

// allows reports whether p is covered by an allowlist entry: same Kind, and
// either a "*" wildcard Target or an exact Target match.
func (a AllowList) allows(p plan.Permission) bool {
	for _, e := range a.entries {
		if e.Kind != p.Kind {
			continue
		}
		if e.Target == "*" || e.Target == p.Target {
			return true
		}
	}
	return false
}

// Intersect returns the subset of requested that the allowlist covers, in
// requested's original order. This is the only path by which Granted
// permissions come into existence (see ApprovedPlan.granted).
func (a AllowList) Intersect(requested []plan.Permission) []plan.Permission {
	var granted []plan.Permission
	for _, r := range requested {
		if a.allows(r) {
			granted = append(granted, r)
		}
	}
	return granted
}
