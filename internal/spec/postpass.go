package spec

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultsPath = "config/spec-defaults.yaml"

var completenessSections = []string{
	"loading",
	"empty",
	"error",
	"validation",
	"permissions",
	"authentication",
	"persistence",
	"apis",
	"responsive",
	"accessibility",
	"analytics",
	"billing",
	"failure",
	"nfr",
}

type Defaults struct {
	AssumedBasis map[string]string `yaml:"assumed_basis"`
}

func LoadDefaults(path string) (Defaults, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultsPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Defaults{}, fmt.Errorf("spec: read defaults %s: %w", path, err)
	}
	var d Defaults
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return Defaults{}, fmt.Errorf("spec: decode defaults %s: %w", path, err)
	}
	return d, nil
}

func PostPass(reqs []Requirement, d Defaults) Specification {
	out := make([]Requirement, 0, len(reqs)+len(completenessSections))
	seenSection := map[string]bool{}

	for _, r := range reqs {
		n := normalizeRequirement(r, d)
		seenSection[n.Section] = true
		out = append(out, n)
	}

	for _, section := range completenessSections {
		if seenSection[section] {
			continue
		}
		out = append(out, Requirement{
			ID:      "auto-unresolved-" + section,
			Section: section,
			Text:    "Unresolved requirement for " + section,
			Label:   LabelUnresolved,
			Basis:   "postpass completeness coverage",
			Impact:  defaultImpact(section),
		})
	}

	sections := uniqueSections(out)
	bySection := map[string][]int{}
	for i := range out {
		bySection[out[i].Section] = append(bySection[out[i].Section], i)
	}

	unresolved := map[Impact]int{}
	for _, r := range out {
		if r.Label == LabelUnresolved {
			unresolved[r.Impact]++
		}
	}

	return Specification{
		Requirements:       out,
		UnresolvedByImpact: unresolved,
		BySection:          bySection,
		Sections:           sections,
	}
}

func normalizeRequirement(r Requirement, d Defaults) Requirement {
	r.Section = strings.TrimSpace(strings.ToLower(r.Section))
	if r.Section == "" {
		r.Section = "nfr"
	}
	r.ID = strings.TrimSpace(r.ID)
	if r.ID == "" {
		r.ID = "req-" + strings.ReplaceAll(r.Section, " ", "-")
	}
	r.Text = strings.TrimSpace(r.Text)
	if r.Text == "" {
		r.Text = "Unresolved requirement"
	}
	if !r.Label.Valid() {
		r.Label = LabelUnresolved
	}
	if !r.Impact.Valid() {
		r.Impact = defaultImpact(r.Section)
	}
	if r.Label == LabelAssumed && strings.TrimSpace(r.Basis) == "" {
		if basis, ok := d.AssumedBasis[r.Section]; ok && strings.TrimSpace(basis) != "" {
			r.Basis = basis
		} else {
			r.Basis = "policy default applied for " + r.Section
		}
	}
	if r.Label != LabelAssumed && strings.TrimSpace(r.Basis) == "" {
		r.Basis = "synthesized"
	}
	return r
}

func defaultImpact(section string) Impact {
	switch section {
	case "authentication", "permissions", "billing", "failure":
		return ImpactHigh
	case "validation", "apis", "persistence":
		return ImpactMedium
	default:
		return ImpactLow
	}
}

func uniqueSections(reqs []Requirement) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		if seen[r.Section] {
			continue
		}
		seen[r.Section] = true
		out = append(out, r.Section)
	}
	sort.Strings(out)
	return out
}
