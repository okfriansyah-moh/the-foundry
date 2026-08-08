package packaging

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	AgentCatalogPath = "agents/catalog.yaml"
	SkillCatalogPath = "skills/catalog.yaml"
)

// LoadCatalogs strictly loads both catalogs rooted at root.
func LoadCatalogs(root string) (Catalogs, error) {
	opened, err := openCanonicalRoot(root)
	if err != nil {
		return Catalogs{}, fmt.Errorf("packaging: open catalog root: %w", err)
	}
	defer func() { _ = opened.Close() }()
	return LoadCatalogsFromRoot(opened)
}

// LoadCatalogsFromRoot reads both catalogs from the already-open repository
// identity. It never resolves root.Name() or reopens the repository pathname.
func LoadCatalogsFromRoot(root *os.Root) (Catalogs, error) {
	var catalogs Catalogs
	if err := decodeCatalogYAMLFileFromRoot(root, AgentCatalogPath, &catalogs.Agents); err != nil {
		return Catalogs{}, fmt.Errorf("packaging: load agent catalog: %w", err)
	}
	if err := decodeCatalogYAMLFileFromRoot(root, SkillCatalogPath, &catalogs.Skills); err != nil {
		return Catalogs{}, fmt.Errorf("packaging: load skill catalog: %w", err)
	}
	return catalogs, nil
}

// ParseCatalogsYAML decodes agent and skill catalog documents from bytes.
func ParseCatalogsYAML(agentRaw, skillRaw []byte) (Catalogs, error) {
	var catalogs Catalogs
	if err := decodeYAMLBytes(AgentCatalogPath, agentRaw, &catalogs.Agents); err != nil {
		return Catalogs{}, fmt.Errorf("packaging: load agent catalog: %w", err)
	}
	if err := decodeYAMLBytes(SkillCatalogPath, skillRaw, &catalogs.Skills); err != nil {
		return Catalogs{}, fmt.Errorf("packaging: load skill catalog: %w", err)
	}
	return catalogs, nil
}

// LoadEnablement strictly loads a product-local enabled.yaml.
func LoadEnablement(path string) (Enablement, error) {
	root, relative, err := openParentRoot(path)
	if err != nil {
		return Enablement{}, fmt.Errorf("packaging: load enablement: %w", err)
	}
	defer func() { _ = root.Close() }()
	return LoadEnablementFromRoot(root, relative)
}

// LoadEnablementFromRoot strictly loads a repository-relative declaration
// beneath an already-open repository identity.
func LoadEnablementFromRoot(root *os.Root, relative string) (Enablement, error) {
	return loadEnablementFromRoot(root, relative, nil)
}

// ParseEnablementYAML decodes one enablement document from bytes.
func ParseEnablementYAML(raw []byte, source string) (Enablement, error) {
	if source == "" {
		source = "enabled.yaml"
	}
	var enabled Enablement
	if err := decodeYAMLBytes(source, raw, &enabled); err != nil {
		return Enablement{}, fmt.Errorf("packaging: load enablement: %w", err)
	}
	return enabled, nil
}

func loadEnablementFromRoot(root *os.Root, relative string, hook canonicalReadHook) (Enablement, error) {
	raw, _, err := readCanonicalFileInfoFromRoot(root, relative, hook)
	if err != nil {
		return Enablement{}, fmt.Errorf("packaging: load enablement: %w", err)
	}
	return ParseEnablementYAML(raw, relative)
}

// LoadProfileEnablement loads package declarations from a profile config while
// allowing the policy compiler to own all other top-level profile fields.
func LoadProfileEnablement(path string) (ProfileEnablement, error) {
	root, relative, err := openParentRoot(path)
	if err != nil {
		return ProfileEnablement{}, fmt.Errorf("packaging: load profile enablement: %w", err)
	}
	defer func() { _ = root.Close() }()
	return LoadProfileEnablementFromRoot(root, relative)
}

// LoadProfileEnablementFromRoot loads package declarations from an
// already-open repository identity without reopening its pathname.
func LoadProfileEnablementFromRoot(root *os.Root, relative string) (ProfileEnablement, error) {
	return loadProfileEnablementFromRoot(root, relative, nil)
}

// ParseProfileEnablementYAML decodes package declarations from one profile YAML payload.
func ParseProfileEnablementYAML(raw []byte, source string) (ProfileEnablement, error) {
	if source == "" {
		source = "profile.yaml"
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s: %w", source, err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s: top level must be a mapping", source)
	}
	var profile ProfileEnablement
	mapping := document.Content[0]
	seenPackageFields := make(map[string]struct{}, 2)
	for i := 0; i < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i].Value, mapping.Content[i+1]
		switch key {
		case "agent_packages":
			if _, duplicate := seenPackageFields[key]; duplicate {
				return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s: duplicate field %q", source, key)
			}
			seenPackageFields[key] = struct{}{}
			if err := decodePackageSelection(value, false, &profile.AgentPackages); err != nil {
				return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s agent_packages: %w", source, err)
			}
		case "skill_packages":
			if _, duplicate := seenPackageFields[key]; duplicate {
				return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s: duplicate field %q", source, key)
			}
			seenPackageFields[key] = struct{}{}
			if err := decodePackageSelection(value, true, &profile.SkillPackages); err != nil {
				return ProfileEnablement{}, fmt.Errorf("packaging: parse profile %s skill_packages: %w", source, err)
			}
		}
	}
	return profile, nil
}

func loadProfileEnablementFromRoot(root *os.Root, relative string, hook canonicalReadHook) (ProfileEnablement, error) {
	raw, _, err := readCanonicalFileInfoFromRoot(root, relative, hook)
	if err != nil {
		return ProfileEnablement{}, fmt.Errorf("packaging: read profile %s: %w", relative, err)
	}
	return ParseProfileEnablementYAML(raw, relative)
}

func decodePackageSelection(node *yaml.Node, allowDomain bool, dst any) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("must be a mapping")
	}
	seen := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = struct{}{}
		if key != "enabled" && !(allowDomain && key == "domain_enabled") {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	if err := node.Decode(dst); err != nil {
		return err
	}
	return nil
}

func decodeCatalogYAMLFileFromRoot(root *os.Root, reference string, dst any) error {
	raw, err := readCanonicalFileFromRoot(root, reference)
	if err != nil {
		return err
	}
	return decodeYAMLBytes(reference, raw, dst)
}

func openParentRoot(path string) (*os.Root, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolve path: %w", err)
	}
	root, err := openCanonicalRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, "", err
	}
	return root, filepath.Base(absolute), nil
}

func decodeYAMLBytes(path string, raw []byte, dst any) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("parse %s: multiple YAML documents are not allowed", path)
		}
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
