package packaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
)

// BuildPackageSet validates the complete catalog and enablement declarations,
// then reads exactly the enabled packages for kind from the canonical root.
func BuildPackageSet(root string, catalogs Catalogs, enabled Enablement, kind agentruntime.Kind) (agentruntime.PackageSet, error) {
	opened, err := openCanonicalRoot(root)
	if err != nil {
		return agentruntime.PackageSet{}, fmt.Errorf("packaging: open catalog root: %w", err)
	}
	defer func() { _ = opened.Close() }()
	return BuildPackageSetFromRoot(opened, catalogs, enabled, kind)
}

// BuildPackageSetFromRoot resolves every validated package byte from one
// already-open repository identity.
func BuildPackageSetFromRoot(root *os.Root, catalogs Catalogs, enabled Enablement, kind agentruntime.Kind) (agentruntime.PackageSet, error) {
	if err := ValidateCatalogsFromRoot(root, catalogs); err != nil {
		return agentruntime.PackageSet{}, err
	}
	if err := ValidateEnablement(catalogs, enabled); err != nil {
		return agentruntime.PackageSet{}, err
	}

	if err := validateInstallClosure(catalogs, enabled); err != nil {
		return agentruntime.PackageSet{}, err
	}
	catalogDigest, enablementDigest, err := inputDigests(catalogs, enabled)
	if err != nil {
		return agentruntime.PackageSet{}, err
	}

	var set agentruntime.PackageSet
	switch kind {
	case agentruntime.KindAgents:
		set, err = buildAgentSetFromRoot(root, catalogs.Agents.Agents, enabled.Agents)
	case agentruntime.KindSkills:
		set, err = buildSkillSetFromRoot(root, catalogs.Skills, enabled)
	default:
		return agentruntime.PackageSet{}, fmt.Errorf("packaging: unsupported install kind %q", kind)
	}
	if err != nil {
		return agentruntime.PackageSet{}, err
	}
	set.CatalogDigest = catalogDigest
	set.EnablementDigest = enablementDigest
	return set, nil
}

// Install validates the complete catalog and enablement closure before asking
// a provider adapter to write the workspace-local projection.
func Install(ctx context.Context, root, workspace string, catalogs Catalogs, enabled Enablement, kind agentruntime.Kind, materializer agentruntime.Materializer) (agentruntime.Result, error) {
	set, err := BuildPackageSet(root, catalogs, enabled, kind)
	if err != nil {
		return agentruntime.Result{}, err
	}
	if materializer == nil {
		return agentruntime.Result{}, fmt.Errorf("packaging: materializer is required")
	}
	return materializer.Install(ctx, workspace, set)
}

// Doctor validates the current canonical inputs before asking the provider to
// verify every installed byte and input pin.
func Doctor(ctx context.Context, root, workspace string, catalogs Catalogs, enabled Enablement, kind agentruntime.Kind, materializer agentruntime.Materializer) (agentruntime.Result, error) {
	set, err := BuildPackageSet(root, catalogs, enabled, kind)
	if err != nil {
		return agentruntime.Result{}, err
	}
	if materializer == nil {
		return agentruntime.Result{}, fmt.Errorf("packaging: materializer is required")
	}
	return materializer.Doctor(ctx, workspace, set)
}

func buildAgentSetFromRoot(root *os.Root, catalog []Agent, enabled []string) (agentruntime.PackageSet, error) {
	byName := make(map[string]Agent, len(catalog))
	for _, agent := range catalog {
		byName[agent.Name] = agent
	}
	set := agentruntime.PackageSet{Kind: agentruntime.KindAgents}
	for _, name := range sortedCopy(enabled) {
		agent := byName[name]
		source, err := readCanonicalFileFromRoot(root, agent.Source)
		if err != nil {
			return agentruntime.PackageSet{}, fmt.Errorf("packaging: read agent %q source: %w", name, err)
		}
		skills := sortedCopy(agent.Skills)
		set.Packages = append(set.Packages, agentruntime.Package{
			Name: name, Description: agent.Description, Skills: skills, Source: source,
		})
	}
	return set, nil
}

func buildSkillSetFromRoot(root *os.Root, catalog SkillCatalog, enabled Enablement) (agentruntime.PackageSet, error) {
	type selectedSkill struct {
		skill  Skill
		domain bool
	}
	byName := make(map[string]selectedSkill, len(catalog.Skills)+len(catalog.DomainSkills))
	for _, skill := range catalog.Skills {
		byName[skill.Name] = selectedSkill{skill: skill}
	}
	for _, skill := range catalog.DomainSkills {
		byName[skill.Name] = selectedSkill{skill: skill, domain: true}
	}

	names := append(append([]string(nil), enabled.Skills...), enabled.DomainSkills...)
	set := agentruntime.PackageSet{Kind: agentruntime.KindSkills}
	for _, name := range sortedCopy(names) {
		selected := byName[name]
		pkg, err := readSkillPackageFromRoot(root, selected.skill, selected.domain, enabled.Profile)
		if err != nil {
			return agentruntime.PackageSet{}, err
		}
		set.Packages = append(set.Packages, pkg)
	}
	return set, nil
}

func readSkillPackageFromRoot(root *os.Root, skill Skill, domain bool, profile string) (agentruntime.Package, error) {
	selectedSource := skill.Source
	var source []byte
	var err error
	if profileSource := skillProfileSourceForProfile(skill, profile); profileSource != nil {
		selectedSource = profileSource.Source
		source, err = readPinnedProfileSourceFromRoot(root, skill.Name, *profileSource)
	} else {
		source, err = readCanonicalFileFromRoot(root, selectedSource)
	}
	if err != nil {
		return agentruntime.Package{}, fmt.Errorf("packaging: read skill %q source: %w", skill.Name, err)
	}
	if filepath.Base(selectedSource) != "SKILL.md" {
		return agentruntime.Package{}, fmt.Errorf("packaging: skill %q source must be named SKILL.md", skill.Name)
	}

	pkg := agentruntime.Package{Name: skill.Name, Description: skill.Description, Domain: domain, Source: source}
	sourceDir := filepath.Dir(filepath.Clean(selectedSource))
	for _, reference := range skill.References {
		rel, err := filepath.Rel(sourceDir, filepath.Clean(reference))
		if err != nil || !safeRelativePath(rel) {
			return agentruntime.Package{}, fmt.Errorf("packaging: skill %q reference %q is outside its package", skill.Name, reference)
		}
		content, err := readCanonicalFileFromRoot(root, reference)
		if err != nil {
			return agentruntime.Package{}, fmt.Errorf("packaging: read skill %q reference %q: %w", skill.Name, reference, err)
		}
		pkg.References = append(pkg.References, agentruntime.File{Path: filepath.ToSlash(rel), Bytes: content})
	}
	sort.Slice(pkg.References, func(i, j int) bool { return pkg.References[i].Path < pkg.References[j].Path })
	return pkg, nil
}

func skillProfileSourceForProfile(skill Skill, profile string) *SkillProfileSource {
	if profile == PersonalAutonomousVentureProfile &&
		skill.ProfileSources != nil &&
		skill.ProfileSources.PersonalAutonomousVenture != nil {
		return skill.ProfileSources.PersonalAutonomousVenture
	}
	return nil
}

func readPinnedProfileSourceFromRoot(root *os.Root, skillName string, source SkillProfileSource) ([]byte, error) {
	content, contentInfo, err := readCanonicalFileInfoFromRoot(root, source.Source, nil)
	if err != nil {
		return nil, err
	}
	if err := validateImmutableProfileObject(source.Source, contentInfo); err != nil {
		return nil, err
	}
	if actual := digestBytes(content); actual != source.SHA256 {
		return nil, fmt.Errorf("packaging: skill %q source digest mismatch: got %s want %s", skillName, actual, source.SHA256)
	}
	metadataPath := filepath.ToSlash(filepath.Join(filepath.Dir(source.Source), "metadata.json"))
	metadata, metadataInfo, err := readCanonicalFileInfoFromRoot(root, metadataPath, nil)
	if err != nil {
		return nil, fmt.Errorf("packaging: skill %q authority metadata: %w", skillName, err)
	}
	if actual := digestBytes(metadata); actual != source.AuthoritySHA256 {
		return nil, fmt.Errorf("packaging: skill %q authority metadata digest mismatch: got %s want %s", skillName, actual, source.AuthoritySHA256)
	}
	if err := validateImmutableProfileObject(metadataPath, metadataInfo); err != nil {
		return nil, err
	}
	if err := validateSkillAuthorityMetadata(skillName, source, content, metadata); err != nil {
		return nil, err
	}
	return content, nil
}

func validateImmutableProfileObject(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("packaging: immutable profile object %q must have exactly one hard link", path)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return fmt.Errorf("packaging: immutable profile object %q is writable", path)
	}
	return nil
}

type skillAuthorityMetadata struct {
	SkillID      string   `json:"skill_id"`
	Version      int      `json:"version"`
	PromptSHA256 string   `json:"prompt_sha256"`
	Permissions  []string `json:"permissions"`
	DataClasses  []string `json:"data_classes"`
	BudgetUSD    float64  `json:"budget_usd"`
}

func validateSkillAuthorityMetadata(skillName string, source SkillProfileSource, content, raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var metadata skillAuthorityMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return fmt.Errorf("packaging: skill %q decode authority metadata: %w", skillName, err)
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("packaging: skill %q encode authority metadata: %w", skillName, err)
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(canonical, raw) {
		return fmt.Errorf("packaging: skill %q authority metadata is not canonical", skillName)
	}
	versionDirectory := filepath.Base(filepath.Dir(source.Source))
	if len(versionDirectory) < 2 || versionDirectory[0] != 'v' {
		return fmt.Errorf("packaging: skill %q authority metadata source version is invalid", skillName)
	}
	version, err := strconv.Atoi(versionDirectory[1:])
	if err != nil || version < 1 {
		return fmt.Errorf("packaging: skill %q authority metadata source version is invalid", skillName)
	}
	if metadata.SkillID != skillName || metadata.Version != version || metadata.PromptSHA256 != digestBytes(content) {
		return fmt.Errorf("packaging: skill %q authority metadata does not bind source identity, version, and content", skillName)
	}
	if metadata.Permissions == nil || metadata.DataClasses == nil || metadata.BudgetUSD < 0 || math.IsNaN(metadata.BudgetUSD) || math.IsInf(metadata.BudgetUSD, 0) {
		return fmt.Errorf("packaging: skill %q authority metadata has invalid authority fields", skillName)
	}
	return nil
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func safeRelativePath(path string) bool {
	clean := filepath.Clean(strings.TrimSpace(path))
	return clean != "." && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func validateInstallClosure(catalogs Catalogs, enabled Enablement) error {
	enabledSkills := make(map[string]struct{}, len(enabled.Skills)+len(enabled.DomainSkills))
	for _, name := range enabled.Skills {
		enabledSkills[name] = struct{}{}
	}
	for _, name := range enabled.DomainSkills {
		enabledSkills[name] = struct{}{}
	}
	enabledAgents := make(map[string]struct{}, len(enabled.Agents))
	for _, name := range enabled.Agents {
		enabledAgents[name] = struct{}{}
	}
	for _, agent := range catalogs.Agents.Agents {
		if _, ok := enabledAgents[agent.Name]; !ok {
			continue
		}
		for _, skill := range agent.Skills {
			if _, ok := enabledSkills[skill]; !ok {
				return fmt.Errorf("packaging: enabled agent %q requires disabled skill %q", agent.Name, skill)
			}
		}
	}
	return nil
}

func inputDigests(catalogs Catalogs, enabled Enablement) (string, string, error) {
	catalogBytes, err := json.Marshal(catalogs)
	if err != nil {
		return "", "", fmt.Errorf("packaging: encode catalog pin: %w", err)
	}
	enabledBytes, err := json.Marshal(enabled)
	if err != nil {
		return "", "", fmt.Errorf("packaging: encode enablement pin: %w", err)
	}
	return digestBytes(catalogBytes), digestBytes(enabledBytes), nil
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
