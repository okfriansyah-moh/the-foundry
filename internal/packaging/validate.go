package packaging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	packageNamePattern         = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	profileSourceSuffixPattern = regexp.MustCompile(`^versions/v[1-9][0-9]*/SKILL\.md$`)
	sha256PinPattern           = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const PersonalAutonomousVentureProfile = "personal-autonomous-venture"

// ValidateCatalogs applies the fail-closed catalog rules and verifies every
// source and reference is a regular file contained by root.
func ValidateCatalogs(root string, catalogs Catalogs) error {
	opened, err := openCanonicalRoot(root)
	if err != nil {
		return fmt.Errorf("packaging: open catalog root: %w", err)
	}
	defer func() { _ = opened.Close() }()
	return ValidateCatalogsFromRoot(opened, catalogs)
}

// ValidateCatalogsFromRoot validates all catalog declarations, sources,
// content pins, and authority metadata beneath one already-open identity.
func ValidateCatalogsFromRoot(root *os.Root, catalogs Catalogs) error {
	if root == nil {
		return fmt.Errorf("packaging: catalog root is required")
	}
	if catalogs.Agents.Version != 1 || catalogs.Skills.Version != 1 {
		return fmt.Errorf("packaging: unsupported catalog version (agents=%d skills=%d)", catalogs.Agents.Version, catalogs.Skills.Version)
	}
	if len(catalogs.Agents.Agents) == 0 || len(catalogs.Skills.Skills) == 0 {
		return fmt.Errorf("packaging: agent and core skill catalogs must not be empty")
	}
	skillNames, err := validateSkillsFromRoot(root, catalogs.Skills)
	if err != nil {
		return err
	}
	agentNames, err := validateAgentsFromRoot(root, catalogs.Agents.Agents, skillNames)
	if err != nil {
		return err
	}
	for name := range agentNames {
		if _, collision := skillNames[name]; collision {
			return fmt.Errorf("packaging: duplicate package name %q across agent and skill catalogs", name)
		}
	}
	return validateBindings(catalogs.Agents.Bindings, agentNames)
}

// ValidateEnablement verifies that every enabled package exists in its catalog.
func ValidateEnablement(catalogs Catalogs, enabled Enablement) error {
	if enabled.Version != 1 {
		return fmt.Errorf("packaging: unsupported enablement version %d", enabled.Version)
	}
	if strings.TrimSpace(enabled.Profile) == "" {
		return fmt.Errorf("packaging: enablement profile is required")
	}
	if len(enabled.Agents) == 0 || len(enabled.Skills) == 0 {
		return fmt.Errorf("packaging: enablement must declare at least one agent and one core skill")
	}
	if err := validateEnabledNames("agent", enabled.Agents, agentNameSet(catalogs.Agents.Agents)); err != nil {
		return err
	}
	if err := validateEnabledNames("skill", enabled.Skills, skillNameSet(catalogs.Skills.Skills)); err != nil {
		return err
	}
	if err := validateEnabledNames("domain skill", enabled.DomainSkills, skillNameSet(catalogs.Skills.DomainSkills)); err != nil {
		return err
	}
	return validateEnabledReviewers(catalogs.Agents, enabled.Agents)
}

// ValidateProfiles proves the organization declaration is a strict subset of
// the personal declaration and that both declarations reference the catalogs.
func ValidateProfiles(catalogs Catalogs, personal, organization ProfileEnablement) error {
	personalEnabled := profileAsEnablement("personal-autonomous-venture", personal)
	organizationEnabled := profileAsEnablement("organization-10x", organization)
	if err := ValidateEnablement(catalogs, personalEnabled); err != nil {
		return fmt.Errorf("packaging: personal profile: %w", err)
	}
	if err := ValidateEnablement(catalogs, organizationEnabled); err != nil {
		return fmt.Errorf("packaging: organization profile: %w", err)
	}
	if err := strictSubset("agents", organizationEnabled.Agents, personalEnabled.Agents); err != nil {
		return err
	}
	if err := strictSubset("skills", organizationEnabled.Skills, personalEnabled.Skills); err != nil {
		return err
	}
	return subset("domain skills", organizationEnabled.DomainSkills, personalEnabled.DomainSkills)
}

func validateSkillsFromRoot(root *os.Root, catalog SkillCatalog) (map[string]struct{}, error) {
	all := make(map[string]struct{}, len(catalog.Skills)+len(catalog.DomainSkills))
	for _, group := range []struct {
		kind     string
		prefix   string
		packages []Skill
	}{
		{kind: "skill", prefix: "skills", packages: catalog.Skills},
		{kind: "domain skill", prefix: "domain-skills", packages: catalog.DomainSkills},
	} {
		for _, skill := range group.packages {
			if err := validatePackageName(group.kind, skill.Name); err != nil {
				return nil, err
			}
			if _, duplicate := all[skill.Name]; duplicate {
				return nil, fmt.Errorf("packaging: duplicate package name %q", skill.Name)
			}
			all[skill.Name] = struct{}{}
			if strings.TrimSpace(skill.Description) == "" {
				return nil, fmt.Errorf("packaging: %s %q has no description", group.kind, skill.Name)
			}
			if err := validateReferenceFromRoot(root, skill.Source, group.prefix); err != nil {
				return nil, fmt.Errorf("packaging: %s %q source: %w", group.kind, skill.Name, err)
			}
			if filepath.Base(filepath.Clean(skill.Source)) != "SKILL.md" {
				return nil, fmt.Errorf("packaging: %s %q source must be named SKILL.md", group.kind, skill.Name)
			}
			for refIndex, ref := range skill.References {
				if err := validateReferenceFromRoot(root, ref, group.prefix); err != nil {
					return nil, fmt.Errorf("packaging: %s %q reference[%d]: %w", group.kind, skill.Name, refIndex, err)
				}
			}
			if err := validateSkillProfileSourcesFromRoot(root, group.kind, group.prefix, skill); err != nil {
				return nil, err
			}
		}
	}
	return all, nil
}

func validateSkillProfileSourcesFromRoot(root *os.Root, kind, prefix string, skill Skill) error {
	if skill.ProfileSources == nil {
		return nil
	}
	override := skill.ProfileSources.PersonalAutonomousVenture
	if override == nil || strings.TrimSpace(override.Source) == "" {
		return fmt.Errorf("packaging: %s %q profile source %q is required", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	if override.Source != filepath.ToSlash(filepath.Clean(strings.TrimSpace(override.Source))) {
		return fmt.Errorf("packaging: %s %q profile source %q must be a canonical relative path", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	if filepath.Base(filepath.Clean(override.Source)) != "SKILL.md" {
		return fmt.Errorf("packaging: %s %q profile source %q must be named SKILL.md", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	if !sha256PinPattern.MatchString(override.SHA256) {
		return fmt.Errorf("packaging: %s %q profile source %q sha256 must be lowercase sha256:<64hex>", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	if !sha256PinPattern.MatchString(override.AuthoritySHA256) {
		return fmt.Errorf("packaging: %s %q profile source %q authority_sha256 must be lowercase sha256:<64hex>", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	if err := validateReferenceFromRoot(root, override.Source, prefix); err != nil {
		return fmt.Errorf("packaging: %s %q profile source %q: %w", kind, skill.Name, PersonalAutonomousVentureProfile, err)
	}
	baselineDir := filepath.Dir(filepath.Clean(skill.Source))
	relativeOverride, err := filepath.Rel(baselineDir, filepath.Clean(override.Source))
	if err != nil || !profileSourceSuffixPattern.MatchString(filepath.ToSlash(relativeOverride)) {
		return fmt.Errorf("packaging: %s %q profile source %q must use %s/versions/v<positive-integer>/SKILL.md", kind, skill.Name, PersonalAutonomousVentureProfile, filepath.ToSlash(baselineDir))
	}
	if _, err := readPinnedProfileSourceFromRoot(root, skill.Name, *override); err != nil {
		return fmt.Errorf("packaging: %s %q profile source %q pins: %w", kind, skill.Name, PersonalAutonomousVentureProfile, err)
	}

	// The catalog currently has one reference list shared by the baseline and
	// profile-specific source. Accepting references here would necessarily mix
	// one version's source with another version's references for at least one
	// profile. Until references are versioned with the source, fail closed.
	if len(skill.References) != 0 {
		return fmt.Errorf("packaging: %s %q profile source %q cannot be combined with unversioned references", kind, skill.Name, PersonalAutonomousVentureProfile)
	}
	return nil
}

func validateAgentsFromRoot(root *os.Root, agents []Agent, skillNames map[string]struct{}) (map[string]Agent, error) {
	names := make(map[string]Agent, len(agents))
	for _, agent := range agents {
		if err := validatePackageName("agent", agent.Name); err != nil {
			return nil, err
		}
		if _, duplicate := names[agent.Name]; duplicate {
			return nil, fmt.Errorf("packaging: duplicate agent name %q", agent.Name)
		}
		names[agent.Name] = agent
		if strings.TrimSpace(agent.Description) == "" {
			return nil, fmt.Errorf("packaging: agent %q has no description", agent.Name)
		}
		if len(agent.Inputs) == 0 || len(agent.Outputs) == 0 {
			return nil, fmt.Errorf("packaging: agent %q must declare inputs and outputs", agent.Name)
		}
		if hasBlank(agent.Inputs) || hasBlank(agent.Outputs) {
			return nil, fmt.Errorf("packaging: agent %q inputs and outputs must not contain blank values", agent.Name)
		}
		if err := validateReferenceFromRoot(root, agent.Source, "agents"); err != nil {
			return nil, fmt.Errorf("packaging: agent %q source: %w", agent.Name, err)
		}
		hasGuardrails := false
		seenSkills := make(map[string]struct{}, len(agent.Skills))
		for _, skill := range agent.Skills {
			if _, duplicate := seenSkills[skill]; duplicate {
				return nil, fmt.Errorf("packaging: agent %q references skill %q more than once", agent.Name, skill)
			}
			seenSkills[skill] = struct{}{}
			if _, ok := skillNames[skill]; !ok {
				return nil, fmt.Errorf("packaging: agent %q references unknown skill %q", agent.Name, skill)
			}
			hasGuardrails = hasGuardrails || skill == "guardrails"
		}
		if agent.WritesProduction && !hasGuardrails {
			return nil, fmt.Errorf("packaging: production-writing agent %q lacks guardrails", agent.Name)
		}
	}
	return names, nil
}

func validateBindings(bindings []TaskBinding, agents map[string]Agent) error {
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if err := validatePackageName("binding", binding.Name); err != nil {
			return err
		}
		if _, duplicate := seen[binding.Name]; duplicate {
			return fmt.Errorf("packaging: duplicate binding name %q", binding.Name)
		}
		seen[binding.Name] = struct{}{}
		if binding.Implementer == binding.Reviewer {
			return fmt.Errorf("packaging: binding %q uses agent %q as both implementer and reviewer", binding.Name, binding.Implementer)
		}
		if _, ok := agents[binding.Implementer]; !ok {
			return fmt.Errorf("packaging: binding %q references unknown implementer %q", binding.Name, binding.Implementer)
		}
		reviewer, ok := agents[binding.Reviewer]
		if !ok {
			return fmt.Errorf("packaging: binding %q references unknown reviewer %q", binding.Name, binding.Reviewer)
		}
		if reviewer.WritesProduction {
			return fmt.Errorf("packaging: binding %q reviewer %q writes production", binding.Name, binding.Reviewer)
		}
	}
	if len(bindings) == 0 {
		return fmt.Errorf("packaging: at least one implementer/reviewer binding is required")
	}
	return nil
}

func hasBlank(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}

func validateReference(root, reference, requiredPrefix string) error {
	opened, err := openCanonicalRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = opened.Close() }()
	return validateReferenceFromRoot(opened, reference, requiredPrefix)
}

func validateReferenceFromRoot(root *os.Root, reference, requiredPrefix string) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(reference))))
	if clean == "." || clean != reference || filepath.IsAbs(filepath.FromSlash(reference)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("reference %q escapes catalog root", reference)
	}
	prefix := requiredPrefix + "/"
	if !strings.HasPrefix(clean, prefix) {
		return fmt.Errorf("reference %q must be under %s", reference, requiredPrefix)
	}
	if _, err := readCanonicalFileFromRoot(root, clean); err != nil {
		return err
	}
	return nil
}

func validatePackageName(kind, name string) error {
	if !packageNamePattern.MatchString(name) {
		return fmt.Errorf("packaging: %s name %q must match %s", kind, name, packageNamePattern)
	}
	return nil
}

func validateEnabledNames(kind string, enabled []string, catalog map[string]struct{}) error {
	seen := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("packaging: duplicate enabled %s %q", kind, name)
		}
		seen[name] = struct{}{}
		if _, exists := catalog[name]; !exists {
			return fmt.Errorf("packaging: enabled %s %q is not in the catalog", kind, name)
		}
	}
	return nil
}

func validateEnabledReviewers(catalog AgentCatalog, enabled []string) error {
	agents := make(map[string]Agent, len(catalog.Agents))
	for _, agent := range catalog.Agents {
		agents[agent.Name] = agent
	}
	enabledAgents := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		enabledAgents[name] = struct{}{}
	}
	for _, binding := range catalog.Bindings {
		if _, implementerEnabled := enabledAgents[binding.Implementer]; !implementerEnabled {
			continue
		}
		if binding.Implementer == binding.Reviewer {
			return fmt.Errorf("packaging: enabled implementer %q in binding %q must have a distinct reviewer", binding.Implementer, binding.Name)
		}
		reviewer, exists := agents[binding.Reviewer]
		if !exists {
			return fmt.Errorf("packaging: enabled implementer %q in binding %q references unknown reviewer %q", binding.Implementer, binding.Name, binding.Reviewer)
		}
		if reviewer.WritesProduction {
			return fmt.Errorf("packaging: enabled implementer %q in binding %q has production-writing reviewer %q", binding.Implementer, binding.Name, binding.Reviewer)
		}
		if _, reviewerEnabled := enabledAgents[binding.Reviewer]; !reviewerEnabled {
			return fmt.Errorf("packaging: enabled implementer %q in binding %q requires enabled reviewer %q", binding.Implementer, binding.Name, binding.Reviewer)
		}
	}
	return nil
}

func strictSubset(kind string, narrower, broader []string) error {
	if err := subset(kind, narrower, broader); err != nil {
		return err
	}
	if len(narrower) >= len(broader) {
		return fmt.Errorf("packaging: organization %s must be narrower than personal %s", kind, kind)
	}
	return nil
}

func subset(kind string, narrower, broader []string) error {
	allowed := make(map[string]struct{}, len(broader))
	for _, name := range broader {
		allowed[name] = struct{}{}
	}
	for _, name := range narrower {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("packaging: organization %s includes %q, which personal does not enable", kind, name)
		}
	}
	return nil
}

func agentNameSet(agents []Agent) map[string]struct{} {
	set := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		set[agent.Name] = struct{}{}
	}
	return set
}

func skillNameSet(skills []Skill) map[string]struct{} {
	set := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		set[skill.Name] = struct{}{}
	}
	return set
}

func profileAsEnablement(name string, profile ProfileEnablement) Enablement {
	return Enablement{
		Version:      1,
		Profile:      name,
		Agents:       profile.AgentPackages.Enabled,
		Skills:       profile.SkillPackages.Enabled,
		DomainSkills: profile.SkillPackages.DomainEnabled,
	}
}
