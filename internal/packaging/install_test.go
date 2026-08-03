package packaging

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
)

type recordingMaterializer struct {
	installed bool
	doctored  bool
}

func (m *recordingMaterializer) Install(_ context.Context, _ string, _ agentruntime.PackageSet) (agentruntime.Result, error) {
	m.installed = true
	return agentruntime.Result{}, nil
}

func (m *recordingMaterializer) Doctor(_ context.Context, _ string, _ agentruntime.PackageSet) (agentruntime.Result, error) {
	m.doctored = true
	return agentruntime.Result{}, nil
}

func TestInstallAndDoctorRefuseInvalidInputsBeforeAdapter(t *testing.T) {
	catalogs := loadRepositoryCatalogs(t)
	enabled := Enablement{Version: 1, Profile: "invalid", Agents: []string{"absent"}, Skills: []string{"guardrails"}}
	materializer := &recordingMaterializer{}
	if _, err := Install(context.Background(), repositoryRoot, t.TempDir(), catalogs, enabled, agentruntime.KindAgents, materializer); err == nil {
		t.Fatal("Install accepted invalid enablement")
	}
	if materializer.installed {
		t.Fatal("Install called adapter before validation")
	}
	if _, err := Doctor(context.Background(), repositoryRoot, t.TempDir(), catalogs, enabled, agentruntime.KindAgents, materializer); err == nil {
		t.Fatal("Doctor accepted invalid enablement")
	}
	if materializer.doctored {
		t.Fatal("Doctor called adapter before validation")
	}
}

func TestInstallRequiresMaterializer(t *testing.T) {
	catalogs := loadRepositoryCatalogs(t)
	enabled, err := LoadEnablement("../../templates/product/.foundry/skills/enabled.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Install(context.Background(), repositoryRoot, t.TempDir(), catalogs, enabled, agentruntime.KindAgents, nil)
	if err == nil || !strings.Contains(err.Error(), "materializer is required") {
		t.Fatalf("Install error = %v, want materializer error", err)
	}
}

func TestBuildPackageSetSelectsOnlyExactPersonalProfileSource(t *testing.T) {
	root, catalogs, enabled := profileSourceFixture(t)
	tests := []struct {
		name    string
		profile string
		want    string
	}{
		{name: "canonical personal profile", profile: PersonalAutonomousVentureProfile, want: "version two\n"},
		{name: "organization profile", profile: "organization-10x", want: "version one\n"},
		{name: "Task 77 evaluation alias is not an enablement profile", profile: "personal", want: "version one\n"},
		{name: "personal profile prefix", profile: PersonalAutonomousVentureProfile + "-preview", want: "version one\n"},
		{name: "personal profile suffix", profile: "preview-" + PersonalAutonomousVentureProfile, want: "version one\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled.Profile = tt.profile
			set, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills)
			if err != nil {
				t.Fatalf("BuildPackageSet: %v", err)
			}
			if len(set.Packages) != 1 || !bytes.Equal(set.Packages[0].Source, []byte(tt.want)) {
				t.Fatalf("source = %q, want %q", set.Packages[0].Source, tt.want)
			}
		})
	}
}

func TestBuildPackageSetCatalogDigestIncludesProfileSource(t *testing.T) {
	root, catalogs, enabled := profileSourceFixture(t)
	enabled.Profile = PersonalAutonomousVentureProfile
	withOverride, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills)
	if err != nil {
		t.Fatal(err)
	}
	catalogs.Skills.Skills[0].ProfileSources = nil
	withoutOverride, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills)
	if err != nil {
		t.Fatal(err)
	}
	if withOverride.CatalogDigest == withoutOverride.CatalogDigest {
		t.Fatal("profile source did not affect catalog digest")
	}
	if bytes.Equal(withOverride.Packages[0].Source, withoutOverride.Packages[0].Source) {
		t.Fatal("profile source did not affect selected package bytes")
	}
}

func TestPinnedProfileSourceTamperFailsBeforeBuildInstallAndDoctor(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(string) error
	}{
		{
			name: "instruction bytes",
			tamper: func(root string) error {
				path := filepath.Join(root, "skills/example/versions/v2/SKILL.md")
				if err := os.Chmod(path, 0o600); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte("tampered prompt\n"), 0o600); err != nil {
					return err
				}
				return os.Chmod(path, 0o400)
			},
		},
		{
			name: "authority metadata",
			tamper: func(root string) error {
				path := filepath.Join(root, "skills/example/versions/v2/metadata.json")
				if err := os.Chmod(path, 0o600); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte("{\"tampered\":true}\n"), 0o600); err != nil {
					return err
				}
				return os.Chmod(path, 0o400)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, catalogs, enabled := profileSourceFixture(t)
			if err := tt.tamper(root); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
				t.Fatalf("BuildPackageSet error = %v, want digest mismatch", err)
			}
			materializer := &recordingMaterializer{}
			if _, err := Install(context.Background(), root, t.TempDir(), catalogs, enabled, agentruntime.KindSkills, materializer); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
				t.Fatalf("Install error = %v, want digest mismatch", err)
			}
			if materializer.installed {
				t.Fatal("Install called materializer after pinned input tamper")
			}
			if _, err := Doctor(context.Background(), root, t.TempDir(), catalogs, enabled, agentruntime.KindSkills, materializer); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
				t.Fatalf("Doctor error = %v, want digest mismatch", err)
			}
			if materializer.doctored {
				t.Fatal("Doctor called materializer after pinned input tamper")
			}
		})
	}
}

func TestPinnedProfileSourceRequiresImmutableSingleLinkObjects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) error
		wantErr string
	}{
		{
			name: "writable instruction",
			mutate: func(root string) error {
				return os.Chmod(filepath.Join(root, "skills/example/versions/v2/SKILL.md"), 0o600)
			},
			wantErr: "is writable",
		},
		{
			name: "writable authority metadata",
			mutate: func(root string) error {
				return os.Chmod(filepath.Join(root, "skills/example/versions/v2/metadata.json"), 0o600)
			},
			wantErr: "is writable",
		},
		{
			name: "hard-linked instruction",
			mutate: func(root string) error {
				return os.Link(filepath.Join(root, "skills/example/versions/v2/SKILL.md"), filepath.Join(root, "linked-skill.md"))
			},
			wantErr: "exactly one hard link",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, catalogs, enabled := profileSourceFixture(t)
			if err := tt.mutate(root); err != nil {
				t.Fatal(err)
			}
			if _, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("BuildPackageSet error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPinnedProfileSourceRejectsSemanticallyInvalidAuthorityMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		wantErr  string
	}{
		{
			name:     "wrong skill identity",
			metadata: "{\"skill_id\":\"other\",\"version\":2,\"prompt_sha256\":\"%s\",\"permissions\":[],\"data_classes\":[],\"budget_usd\":0}\n",
			wantErr:  "does not bind",
		},
		{
			name:     "wrong version",
			metadata: "{\"skill_id\":\"example\",\"version\":3,\"prompt_sha256\":\"%s\",\"permissions\":[],\"data_classes\":[],\"budget_usd\":0}\n",
			wantErr:  "does not bind",
		},
		{
			name:     "unknown field",
			metadata: "{\"skill_id\":\"example\",\"version\":2,\"prompt_sha256\":\"%s\",\"permissions\":[],\"data_classes\":[],\"budget_usd\":0,\"extra\":true}\n",
			wantErr:  "unknown field",
		},
		{
			name:     "noncanonical JSON",
			metadata: "{ \"skill_id\": \"example\", \"version\": 2, \"prompt_sha256\": \"%s\", \"permissions\": [], \"data_classes\": [], \"budget_usd\": 0 }\n",
			wantErr:  "not canonical",
		},
		{
			name:     "null authority sets",
			metadata: "{\"skill_id\":\"example\",\"version\":2,\"prompt_sha256\":\"%s\",\"permissions\":null,\"data_classes\":null,\"budget_usd\":0}\n",
			wantErr:  "invalid authority fields",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, catalogs, enabled := profileSourceFixture(t)
			pin := catalogs.Skills.Skills[0].ProfileSources.PersonalAutonomousVenture
			raw := []byte(fmt.Sprintf(tt.metadata, pin.SHA256))
			metadataPath := filepath.Join(root, "skills/example/versions/v2/metadata.json")
			if err := os.Chmod(metadataPath, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(metadataPath, 0o400); err != nil {
				t.Fatal(err)
			}
			pin.AuthoritySHA256 = digestBytes(raw)
			if _, err := BuildPackageSet(root, catalogs, enabled, agentruntime.KindSkills); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("BuildPackageSet error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func profileSourceFixture(t *testing.T) (string, Catalogs, Enablement) {
	t.Helper()
	root := t.TempDir()
	versionTwo := "version two\n"
	versionTwoDigest := digestBytes([]byte(versionTwo))
	metadata := "{\"skill_id\":\"example\",\"version\":2,\"prompt_sha256\":\"" + versionTwoDigest + "\",\"permissions\":[],\"data_classes\":[],\"budget_usd\":0}\n"
	files := map[string]string{
		"agents/implementer.md":                    "implementer\n",
		"agents/reviewer.md":                       "reviewer\n",
		"skills/example/SKILL.md":                  "version one\n",
		"skills/example/versions/v2/SKILL.md":      versionTwo,
		"skills/example/versions/v2/metadata.json": metadata,
		"skills/example/versions/v2/reference.md":  "version two reference\n",
	}
	for path, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"skills/example/versions/v2/SKILL.md", "skills/example/versions/v2/metadata.json"} {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(path)), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	override := &SkillProfileSource{
		Source:          "skills/example/versions/v2/SKILL.md",
		SHA256:          versionTwoDigest,
		AuthoritySHA256: digestBytes([]byte(metadata)),
	}
	catalogs := Catalogs{
		Agents: AgentCatalog{
			Version: 1,
			Agents: []Agent{
				{Name: "implementer", Description: "implements", Source: "agents/implementer.md", Skills: []string{"example"}, Inputs: []string{"task"}, Outputs: []string{"change"}},
				{Name: "reviewer", Description: "reviews", Source: "agents/reviewer.md", Inputs: []string{"change"}, Outputs: []string{"review"}},
			},
			Bindings: []TaskBinding{{Name: "example-task", Implementer: "implementer", Reviewer: "reviewer"}},
		},
		Skills: SkillCatalog{Version: 1, Skills: []Skill{{
			Name: "example", Description: "example skill", Source: "skills/example/SKILL.md",
			ProfileSources: &SkillProfileSources{PersonalAutonomousVenture: override},
		}}},
	}
	enabled := Enablement{Version: 1, Profile: PersonalAutonomousVentureProfile, Agents: []string{"implementer", "reviewer"}, Skills: []string{"example"}}
	return root, catalogs, enabled
}
