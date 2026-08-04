package packaging

// AgentCatalog is the provider-neutral catalog stored at agents/catalog.yaml.
type AgentCatalog struct {
	Version  int           `yaml:"version"`
	Agents   []Agent       `yaml:"agents"`
	Bindings []TaskBinding `yaml:"bindings"`
}

// Agent declares one bounded role and the skills and artifacts it consumes.
type Agent struct {
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	Source           string   `yaml:"source"`
	Skills           []string `yaml:"skills"`
	Inputs           []string `yaml:"inputs"`
	Outputs          []string `yaml:"outputs"`
	WritesProduction bool     `yaml:"writes_production"`
}

// TaskBinding declares the independent implementer and reviewer for a task class.
type TaskBinding struct {
	Name        string `yaml:"name"`
	Implementer string `yaml:"implementer"`
	Reviewer    string `yaml:"reviewer"`
}

// SkillCatalog is the provider-neutral catalog stored at skills/catalog.yaml.
type SkillCatalog struct {
	Version      int     `yaml:"version"`
	Skills       []Skill `yaml:"skills"`
	DomainSkills []Skill `yaml:"domain_skills"`
}

// Skill declares one reusable instruction package and its optional references.
type Skill struct {
	Name           string               `yaml:"name"`
	Description    string               `yaml:"description"`
	Source         string               `yaml:"source"`
	ProfileSources *SkillProfileSources `yaml:"profile_sources,omitempty"`
	References     []string             `yaml:"references"`
}

// SkillProfileSources holds the deliberately narrow set of profile-specific
// current-version pointers. A struct (rather than a free-form map) makes the
// strict YAML loader reject misspelled, fuzzy, and newly invented profile
// names. The pointer field distinguishes an absent override from an explicitly
// empty override, which must fail closed.
type SkillProfileSources struct {
	PersonalAutonomousVenture *SkillProfileSource `yaml:"personal-autonomous-venture"`
}

// SkillProfileSource is a content-addressed current-version pointer. SHA256
// pins the instruction bytes; AuthoritySHA256 independently pins the adjacent
// immutable metadata.json containing the promotion's authority envelope.
type SkillProfileSource struct {
	Source          string `yaml:"source"`
	SHA256          string `yaml:"sha256"`
	AuthoritySHA256 string `yaml:"authority_sha256"`
}

// Enablement is a product-local declaration. It does not install packages or
// grant the listed agents any runtime authority.
type Enablement struct {
	Version      int      `yaml:"version"`
	Profile      string   `yaml:"profile"`
	Agents       []string `yaml:"agents"`
	Skills       []string `yaml:"skills"`
	DomainSkills []string `yaml:"domain_skills"`
}

// ProfileEnablement is the package-only portion of a Foundry profile config.
type ProfileEnablement struct {
	AgentPackages struct {
		Enabled []string `yaml:"enabled"`
	} `yaml:"agent_packages"`
	SkillPackages struct {
		Enabled       []string `yaml:"enabled"`
		DomainEnabled []string `yaml:"domain_enabled"`
	} `yaml:"skill_packages"`
}

// Catalogs is a fully loaded catalog pair.
type Catalogs struct {
	Agents AgentCatalog
	Skills SkillCatalog
}
