package packaging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
	"gopkg.in/yaml.v3"
)

func TestOpenedRootAPIsKeepOneRepositoryIdentityAfterPathReplacement(t *testing.T) {
	rootPath, catalogs, enabled := profileSourceFixture(t)
	writeRootAPIFixture(t, rootPath, catalogs, enabled)
	root, err := openCanonicalRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	moved := rootPath + "-opened"
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Rename(rootPath, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootPath, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootPath, "skills/example/versions/v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"agents/catalog.yaml":                      "version: 99\nagents: []\nbindings: []\n",
		"skills/catalog.yaml":                      "version: 99\nskills: []\ndomain_skills: []\n",
		"skills/example/SKILL.md":                  "replacement baseline\n",
		"skills/example/versions/v2/SKILL.md":      "replacement attacker bytes\n",
		"skills/example/versions/v2/metadata.json": "{\"replacement\":true}\n",
	} {
		absolute := filepath.Join(rootPath, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := LoadCatalogsFromRoot(root)
	if err != nil {
		t.Fatalf("LoadCatalogsFromRoot reopened replacement path: %v", err)
	}
	if loaded.Agents.Version != 1 || loaded.Skills.Version != 1 {
		t.Fatalf("loaded replacement catalogs: agents=%d skills=%d", loaded.Agents.Version, loaded.Skills.Version)
	}
	loadedEnabled, err := LoadEnablementFromRoot(root, "templates/product/.foundry/skills/enabled.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if loadedEnabled.Profile != PersonalAutonomousVentureProfile {
		t.Fatalf("enablement profile = %q", loadedEnabled.Profile)
	}
	if _, err := LoadProfileEnablementFromRoot(root, "config/profiles/personal-autonomous-venture.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalogsFromRoot(root, loaded); err != nil {
		t.Fatalf("ValidateCatalogsFromRoot: %v", err)
	}
	set, err := BuildPackageSetFromRoot(root, loaded, loadedEnabled, agentruntime.KindSkills)
	if err != nil {
		t.Fatalf("BuildPackageSetFromRoot: %v", err)
	}
	if len(set.Packages) != 1 || !bytes.Equal(set.Packages[0].Source, []byte("version two\n")) {
		t.Fatalf("package source = %q, want opened-root v2", set.Packages[0].Source)
	}
}

func TestOpenedRootEnablementAndProfileRejectSymlinks(t *testing.T) {
	rootPath := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "enabled.yaml"), []byte("version: 1\nprofile: personal\nagents: [a]\nskills: [s]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "profile.yaml"), []byte("agent_packages:\n  enabled: [a]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rootPath, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "enabled.yaml"), filepath.Join(rootPath, "config/enabled.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "profile.yaml"), filepath.Join(rootPath, "config/profile.yaml")); err != nil {
		t.Fatal(err)
	}
	root, err := openCanonicalRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if _, err := LoadEnablementFromRoot(root, "config/enabled.yaml"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadEnablementFromRoot error = %v, want symlink rejection", err)
	}
	if _, err := LoadProfileEnablementFromRoot(root, "config/profile.yaml"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadProfileEnablementFromRoot error = %v, want symlink rejection", err)
	}
}

func TestOpenedRootYAMLLoadersRejectDeterministicSwaps(t *testing.T) {
	t.Run("enablement parent directory", func(t *testing.T) {
		rootPath := t.TempDir()
		directory := filepath.Join(rootPath, "config/product")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "enabled.yaml"), []byte("version: 1\nprofile: trusted\nagents: [a]\nskills: [s]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "enabled.yaml"), []byte("version: 1\nprofile: attacker\nagents: [a]\nskills: [s]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := openCanonicalRoot(rootPath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		swapped := false
		enabled, err := loadEnablementFromRoot(root, "config/product/enabled.yaml", func(stage, path string) error {
			if swapped || stage != "after-parent-lstat" || path != "config/product" {
				return nil
			}
			swapped = true
			if err := os.Rename(directory, filepath.Join(rootPath, "config/original")); err != nil {
				return err
			}
			return os.Symlink(outside, directory)
		})
		if !swapped || err == nil || enabled.Profile == "attacker" {
			t.Fatalf("enabled=%#v err=%v swapped=%t; attacker replacement must not load", enabled, err, swapped)
		}
	})

	t.Run("profile final file", func(t *testing.T) {
		rootPath := t.TempDir()
		directory := filepath.Join(rootPath, "config")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "profile.yaml")
		if err := os.WriteFile(path, []byte("agent_packages:\n  enabled: [trusted]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		replacement := filepath.Join(directory, "replacement.yaml")
		if err := os.WriteFile(replacement, []byte("agent_packages:\n  enabled: [attacker]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := openCanonicalRoot(rootPath)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		profile, err := loadProfileEnablementFromRoot(root, "config/profile.yaml", func(stage, _ string) error {
			if stage != "after-file-lstat" {
				return nil
			}
			if err := os.Rename(path, filepath.Join(directory, "original.yaml")); err != nil {
				return err
			}
			return os.Rename(replacement, path)
		})
		if err == nil || len(profile.AgentPackages.Enabled) != 0 {
			t.Fatalf("profile=%#v err=%v; replacement file must not load", profile, err)
		}
	})
}

func writeRootAPIFixture(t *testing.T, root string, catalogs Catalogs, enabled Enablement) {
	t.Helper()
	agents, err := yaml.Marshal(catalogs.Agents)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := yaml.Marshal(catalogs.Skills)
	if err != nil {
		t.Fatal(err)
	}
	enablement, err := yaml.Marshal(enabled)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		AgentCatalogPath: agents,
		SkillCatalogPath: skills,
		"templates/product/.foundry/skills/enabled.yaml":   enablement,
		"config/profiles/personal-autonomous-venture.yaml": []byte("agent_packages:\n  enabled: [implementer, reviewer]\nskill_packages:\n  enabled: [example]\n  domain_enabled: []\n"),
	}
	for path, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
