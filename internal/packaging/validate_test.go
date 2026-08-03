package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repositoryRoot = "../.."

func TestRepositoryCatalogsValidate(t *testing.T) {
	catalogs := loadRepositoryCatalogs(t)
	if err := ValidateCatalogs(repositoryRoot, catalogs); err != nil {
		t.Fatalf("ValidateCatalogs: %v", err)
	}
	enabled, err := LoadEnablement(filepath.Join(repositoryRoot, "templates/product/.foundry/skills/enabled.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateEnablement(catalogs, enabled); err != nil {
		t.Fatalf("ValidateEnablement: %v", err)
	}
	personal, err := LoadProfileEnablement(filepath.Join(repositoryRoot, "config/profiles/personal-autonomous-venture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	organization, err := LoadProfileEnablement(filepath.Join(repositoryRoot, "config/profiles/organization-10x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfiles(catalogs, personal, organization); err != nil {
		t.Fatalf("ValidateProfiles: %v", err)
	}
}

func TestValidateCatalogsFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Catalogs)
		wantErr string
	}{
		{"missing source", func(c *Catalogs) { c.Agents.Agents[0].Source = "agents/missing.md" }, "inspect reference"},
		{"missing reference", func(c *Catalogs) { c.Skills.Skills[0].References = []string{"skills/guardrails/missing.md"} }, "inspect reference"},
		{"duplicate agent", func(c *Catalogs) { c.Agents.Agents[1].Name = c.Agents.Agents[0].Name }, "duplicate agent"},
		{"duplicate skill", func(c *Catalogs) { c.Skills.Skills[1].Name = c.Skills.Skills[0].Name }, "duplicate package"},
		{"agent skill collision", func(c *Catalogs) {
			collision := c.Skills.Skills[0]
			collision.Name = c.Agents.Agents[0].Name
			c.Skills.Skills = append(c.Skills.Skills, collision)
		}, "across agent and skill"},
		{"missing description", func(c *Catalogs) { c.Skills.Skills[0].Description = " " }, "no description"},
		{"missing agent input", func(c *Catalogs) { c.Agents.Agents[0].Inputs = nil }, "inputs and outputs"},
		{"missing agent output", func(c *Catalogs) { c.Agents.Agents[0].Outputs = nil }, "inputs and outputs"},
		{"unknown agent skill", func(c *Catalogs) { c.Agents.Agents[0].Skills = append(c.Agents.Agents[0].Skills, "unknown") }, "unknown skill"},
		{"production writer without guardrails", func(c *Catalogs) { c.Agents.Agents[1].Skills = []string{"stop-slop"} }, "lacks guardrails"},
		{"reviewer equals implementer", func(c *Catalogs) { c.Agents.Bindings[0].Reviewer = c.Agents.Bindings[0].Implementer }, "both implementer and reviewer"},
		{"reviewer writes production", func(c *Catalogs) { c.Agents.Bindings[0].Reviewer = "pec" }, "writes production"},
		{"unknown reviewer", func(c *Catalogs) { c.Agents.Bindings[0].Reviewer = "absent" }, "unknown reviewer"},
		{"traversal", func(c *Catalogs) { c.Agents.Agents[0].Source = "../outside.md" }, "escapes catalog root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalogs := loadRepositoryCatalogs(t)
			tt.mutate(&catalogs)
			err := ValidateCatalogs(repositoryRoot, catalogs)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateCatalogs error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCatalogsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "agents", "escape.md")); err != nil {
		t.Fatal(err)
	}
	if err := validateReference(root, "agents/escape.md", "agents"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("validateReference error = %v, want symlink escape rejection", err)
	}
}

func TestLoadCatalogsRejectsFixedCatalogSymlinkEscape(t *testing.T) {
	tests := []struct {
		name        string
		catalogPath string
	}{
		{name: "agent catalog", catalogPath: AgentCatalogPath},
		{name: "skill catalog", catalogPath: SkillCatalogPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "agents"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{AgentCatalogPath, SkillCatalogPath} {
				if path == tt.catalogPath {
					continue
				}
				if err := os.WriteFile(filepath.Join(root, path), []byte("version: 1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			outside := filepath.Join(t.TempDir(), "catalog.yaml")
			if err := os.WriteFile(outside, []byte("version: 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, tt.catalogPath)); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadCatalogs(root); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("LoadCatalogs error = %v, want symlink escape rejection", err)
			}
		})
	}
}

func TestLoadCatalogsRejectsNonRegularFixedCatalog(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, AgentCatalogPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalogs(root); err == nil || !strings.Contains(err.Error(), "non-regular file") {
		t.Fatalf("LoadCatalogs error = %v, want regular-file rejection", err)
	}
}

func TestValidateEnablementFailsUnknownAndDuplicates(t *testing.T) {
	catalogs := loadRepositoryCatalogs(t)
	tests := []struct {
		name    string
		enabled Enablement
		wantErr string
	}{
		{"unknown agent", Enablement{Version: 1, Profile: "test", Agents: []string{"absent"}, Skills: []string{"guardrails"}}, "not in the catalog"},
		{"unknown skill", Enablement{Version: 1, Profile: "test", Agents: []string{"planning"}, Skills: []string{"absent"}}, "not in the catalog"},
		{"unknown domain skill", Enablement{Version: 1, Profile: "test", Agents: []string{"planning"}, Skills: []string{"guardrails"}, DomainSkills: []string{"absent"}}, "not in the catalog"},
		{"duplicate", Enablement{Version: 1, Profile: "test", Agents: []string{"planning"}, Skills: []string{"guardrails", "guardrails"}}, "duplicate enabled"},
		{"missing profile", Enablement{Version: 1}, "profile is required"},
		{"unknown version", Enablement{Version: 2, Profile: "test"}, "unsupported enablement version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnablement(catalogs, tt.enabled)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateEnablement error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEnablementRequiresEnabledIndependentReviewer(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Catalogs)
		agents  []string
		wantErr string
	}{
		{
			name:    "reviewer disabled",
			agents:  []string{"implementation"},
			wantErr: "requires enabled reviewer",
		},
		{
			name: "reviewer is implementer",
			mutate: func(c *Catalogs) {
				c.Agents.Bindings[0].Reviewer = c.Agents.Bindings[0].Implementer
			},
			agents:  []string{"implementation"},
			wantErr: "distinct reviewer",
		},
		{
			name: "reviewer writes production",
			mutate: func(c *Catalogs) {
				c.Agents.Bindings[0].Reviewer = "pec"
			},
			agents:  []string{"implementation", "pec"},
			wantErr: "production-writing reviewer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalogs := loadRepositoryCatalogs(t)
			if tt.mutate != nil {
				tt.mutate(&catalogs)
			}
			enabled := Enablement{
				Version: 1,
				Profile: "test",
				Agents:  tt.agents,
				Skills:  []string{"guardrails"},
			}
			err := ValidateEnablement(catalogs, enabled)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateEnablement error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadProfileEnablementRejectsDuplicatePackageKeys(t *testing.T) {
	tests := []struct {
		name    string
		profile string
	}{
		{
			name: "top-level agent packages",
			profile: "agent_packages:\n  enabled: [planning]\n" +
				"agent_packages:\n  enabled: [reviewer]\n",
		},
		{
			name: "top-level skill packages",
			profile: "skill_packages:\n  enabled: [guardrails]\n" +
				"skill_packages:\n  enabled: [testing]\n",
		},
		{
			name:    "nested agent enabled",
			profile: "agent_packages:\n  enabled: [planning]\n  enabled: [reviewer]\n",
		},
		{
			name:    "nested skill enabled",
			profile: "skill_packages:\n  enabled: [guardrails]\n  enabled: [testing]\n",
		},
		{
			name:    "nested domain enabled",
			profile: "skill_packages:\n  domain_enabled: [release-verification]\n  domain_enabled: [commercial-readiness]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.yaml")
			if err := os.WriteFile(path, []byte(tt.profile), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProfileEnablement(path); err == nil || !strings.Contains(err.Error(), "duplicate field") {
				t.Fatalf("LoadProfileEnablement error = %v, want duplicate-field rejection", err)
			}
		})
	}
}

func TestValidateProfilesRequiresNarrowOrganizationSelection(t *testing.T) {
	catalogs := loadRepositoryCatalogs(t)
	personal, organization := repositoryProfiles(t)
	organization.AgentPackages.Enabled = append(organization.AgentPackages.Enabled, "pec", "backend")
	if err := ValidateProfiles(catalogs, personal, organization); err == nil || !strings.Contains(err.Error(), "must be narrower") {
		t.Fatalf("ValidateProfiles error = %v, want strict-subset rejection", err)
	}
	personal, organization = repositoryProfiles(t)
	organization.SkillPackages.Enabled = append(organization.SkillPackages.Enabled, "absent")
	if err := ValidateProfiles(catalogs, personal, organization); err == nil || !strings.Contains(err.Error(), "not in the catalog") {
		t.Fatalf("ValidateProfiles error = %v, want unknown package rejection", err)
	}
}

func TestLoadCatalogsRejectsUnknownAndDuplicateYAMLKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, AgentCatalogPath), []byte("version: 1\nversion: 1\nagents: []\nbindings: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SkillCatalogPath), []byte("version: 1\nskills: []\ndomain_skills: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalogs(root); err == nil {
		t.Fatal("duplicate YAML key must fail closed")
	}
	if err := os.WriteFile(filepath.Join(root, AgentCatalogPath), []byte("version: 1\nagents: []\nbindings: []\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalogs(root); err == nil {
		t.Fatal("unknown YAML key must fail closed")
	}
}

func TestLoadCatalogsRejectsUnknownDuplicateAndMalformedProfileSources(t *testing.T) {
	tests := []struct {
		name          string
		profileSource string
	}{
		{name: "unknown profile", profileSource: "profile_sources:\n      organization-10x: skills/example/versions/v2/SKILL.md"},
		{name: "fuzzy personal profile", profileSource: "profile_sources:\n      personal: skills/example/versions/v2/SKILL.md"},
		{name: "duplicate profile", profileSource: "profile_sources:\n      personal-autonomous-venture: skills/example/versions/v2/SKILL.md\n      personal-autonomous-venture: skills/example/versions/v3/SKILL.md"},
		{name: "malformed scalar", profileSource: "profile_sources: skills/example/versions/v2/SKILL.md"},
		{name: "duplicate container", profileSource: "profile_sources:\n      personal-autonomous-venture: skills/example/versions/v2/SKILL.md\n    profile_sources:\n      personal-autonomous-venture: skills/example/versions/v3/SKILL.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeCatalogDocuments(t, "version: 1\nskills:\n  - name: example\n    description: example\n    source: skills/example/SKILL.md\n    "+tt.profileSource+"\n    references: []\ndomain_skills: []\n")
			if _, err := LoadCatalogs(root); err == nil {
				t.Fatal("LoadCatalogs accepted invalid profile_sources")
			}
		})
	}
}

func TestLoadCatalogsParsesContentAddressedProfileSource(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	authorityDigest := "sha256:" + strings.Repeat("b", 64)
	root := writeCatalogDocuments(t, "version: 1\nskills:\n  - name: example\n    description: example\n    source: skills/example/SKILL.md\n    profile_sources:\n      personal-autonomous-venture:\n        source: skills/example/versions/v2/SKILL.md\n        sha256: "+digest+"\n        authority_sha256: "+authorityDigest+"\n    references: []\ndomain_skills: []\n")
	catalogs, err := LoadCatalogs(root)
	if err != nil {
		t.Fatal(err)
	}
	pin := catalogs.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture
	if pin == nil || pin.Source != "skills/example/versions/v2/SKILL.md" || pin.SHA256 != digest || pin.AuthoritySHA256 != authorityDigest {
		t.Fatalf("profile source pin = %#v", pin)
	}
}

func TestLoadCatalogsRejectsUnknownProfileSourcePinField(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	root := writeCatalogDocuments(t, "version: 1\nskills:\n  - name: example\n    description: example\n    source: skills/example/SKILL.md\n    profile_sources:\n      personal-autonomous-venture:\n        source: skills/example/versions/v2/SKILL.md\n        sha256: "+digest+"\n        authority_sha256: "+digest+"\n        digest: "+digest+"\n    references: []\ndomain_skills: []\n")
	if _, err := LoadCatalogs(root); err == nil || !strings.Contains(err.Error(), "field digest not found") {
		t.Fatalf("LoadCatalogs error = %v, want unknown nested pin field rejection", err)
	}
}

func TestValidateCatalogsRejectsInvalidProfileSources(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Catalogs, string)
		wantErr string
	}{
		{
			name: "empty override",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture = &SkillProfileSource{Source: " "}
			},
			wantErr: "is required",
		},
		{
			name: "missing override field",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture = nil
			},
			wantErr: "is required",
		},
		{
			name: "missing content pin",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.SHA256 = ""
			},
			wantErr: "sha256 must be lowercase",
		},
		{
			name: "uppercase content pin",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.SHA256 = "sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			},
			wantErr: "sha256 must be lowercase",
		},
		{
			name: "bare content digest",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.SHA256 = strings.Repeat("a", 64)
			},
			wantErr: "sha256 must be lowercase",
		},
		{
			name: "missing authority pin",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.AuthoritySHA256 = ""
			},
			wantErr: "authority_sha256 must be lowercase",
		},
		{
			name: "uppercase authority pin",
			mutate: func(c *Catalogs, _ string) {
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.AuthoritySHA256 = "sha256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
			},
			wantErr: "authority_sha256 must be lowercase",
		},
		{
			name: "path traversal",
			mutate: func(c *Catalogs, _ string) {
				path := "../outside/SKILL.md"
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = path
			},
			wantErr: "escapes catalog root",
		},
		{
			name: "noncanonical path",
			mutate: func(c *Catalogs, _ string) {
				path := "skills/example/versions/v2/../v2/SKILL.md"
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = path
			},
			wantErr: "canonical relative path",
		},
		{
			name: "wrong source name",
			mutate: func(c *Catalogs, root string) {
				path := "skills/example/versions/v2/instructions.md"
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("instructions"), 0o600); err != nil {
					t.Fatal(err)
				}
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = path
			},
			wantErr: "must be named SKILL.md",
		},
		{
			name: "zero version",
			mutate: func(c *Catalogs, root string) {
				path := "skills/example/versions/v0/SKILL.md"
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(path))), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("zero"), 0o600); err != nil {
					t.Fatal(err)
				}
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = path
			},
			wantErr: "v<positive-integer>",
		},
		{
			name: "different package directory",
			mutate: func(c *Catalogs, root string) {
				path := "skills/other/versions/v2/SKILL.md"
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(path))), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("other"), 0o600); err != nil {
					t.Fatal(err)
				}
				c.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = path
			},
			wantErr: "v<positive-integer>",
		},
		{
			name: "baseline reference not snapshotted",
			mutate: func(c *Catalogs, root string) {
				path := filepath.Join(root, "skills/example/reference.md")
				if err := os.WriteFile(path, []byte("baseline reference"), 0o600); err != nil {
					t.Fatal(err)
				}
				c.Skills.Skills[0].References = []string{"skills/example/reference.md"}
			},
			wantErr: "cannot be combined with unversioned references",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, catalogs, _ := profileSourceFixture(t)
			tt.mutate(&catalogs, root)
			err := ValidateCatalogs(root, catalogs)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateCatalogs error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCatalogsRejectsProfileSourceSymlinkEscape(t *testing.T) {
	root, catalogs, _ := profileSourceFixture(t)
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "skills/example/versions/escape")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Fatal(err)
	}
	override := "skills/example/versions/escape/SKILL.md"
	catalogs.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = override
	if err := ValidateCatalogs(root, catalogs); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ValidateCatalogs error = %v, want symlink escape rejection", err)
	}
}

func TestValidateCatalogsRejectsProfileSourceInternalSymlink(t *testing.T) {
	root, catalogs, _ := profileSourceFixture(t)
	target := filepath.Join(root, "skills/example/versions/v2")
	link := filepath.Join(root, "skills/example/versions/current")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	override := "skills/example/versions/current/SKILL.md"
	catalogs.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture.Source = override
	if err := ValidateCatalogs(root, catalogs); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ValidateCatalogs error = %v, want internal symlink rejection", err)
	}
}

func writeCatalogDocuments(t *testing.T, skillCatalog string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		AgentCatalogPath: "version: 1\nagents: []\nbindings: []\n",
		SkillCatalogPath: skillCatalog,
	} {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadRepositoryCatalogs(t *testing.T) Catalogs {
	t.Helper()
	catalogs, err := LoadCatalogs(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	return catalogs
}

func repositoryProfiles(t *testing.T) (ProfileEnablement, ProfileEnablement) {
	t.Helper()
	personal, err := LoadProfileEnablement(filepath.Join(repositoryRoot, "config/profiles/personal-autonomous-venture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	organization, err := LoadProfileEnablement(filepath.Join(repositoryRoot, "config/profiles/organization-10x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return personal, organization
}
