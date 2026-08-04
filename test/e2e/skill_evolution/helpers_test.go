package skillevolution_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
	"github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime/claudecode"
	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func copyCapabilityRoot(t *testing.T, sourceRoot string) string {
	t.Helper()
	destination := t.TempDir()
	for _, path := range []string{"agents", "skills", "domain-skills"} {
		copyTree(t, filepath.Join(sourceRoot, path), filepath.Join(destination, path))
	}
	enabled := filepath.Join("templates", "product", ".foundry", "skills", "enabled.yaml")
	copyRegularFile(t, filepath.Join(sourceRoot, enabled), filepath.Join(destination, enabled))
	for _, profile := range []string{"personal-autonomous-venture.yaml", "organization-10x.yaml"} {
		relative := filepath.Join("config", "profiles", profile)
		copyRegularFile(t, filepath.Join(sourceRoot, relative), filepath.Join(destination, relative))
	}
	return destination
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink in capability fixture: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular fixture file: %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy capability tree %s: %v", source, err)
	}
}

func copyRegularFile(t *testing.T, source, destination string) {
	t.Helper()
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatalf("stat fixture %s: %v", source, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("fixture must be a regular file: %s", source)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read fixture %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(destination, content, info.Mode().Perm()); err != nil {
		t.Fatalf("write fixture %s: %v", destination, err)
	}
}

func installAndDoctorSkill(t *testing.T, root, workspace string) []byte {
	t.Helper()
	return installAndDoctorSkillForProfile(t, root, workspace, packaging.PersonalAutonomousVentureProfile)
}

func installAndDoctorSkillForProfile(t *testing.T, root, workspace, profile string) []byte {
	t.Helper()
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create product workspace: %v", err)
	}
	catalogs, err := packaging.LoadCatalogs(root)
	if err != nil {
		t.Fatalf("load catalogs: %v", err)
	}
	enabledPath := filepath.Join(root, "templates", "product", ".foundry", "skills", "enabled.yaml")
	enabled, err := packaging.LoadEnablement(enabledPath)
	if err != nil {
		t.Fatalf("load enablement: %v", err)
	}
	enabled.Profile = profile
	materializer := claudecode.Materializer{}
	if _, err := packaging.Install(context.Background(), root, workspace, catalogs, enabled, agentruntime.KindSkills, materializer); err != nil {
		t.Fatalf("install skills: %v", err)
	}
	if _, err := packaging.Doctor(context.Background(), root, workspace, catalogs, enabled, agentruntime.KindSkills, materializer); err != nil {
		t.Fatalf("doctor skills: %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(workspace, ".claude", "skills", "code-reviewer-correctness", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed evolved skill: %v", err)
	}
	return installed
}

func digestPaths(t *testing.T, root string, paths ...string) string {
	t.Helper()
	var rows []string
	for _, relativeRoot := range paths {
		absoluteRoot := filepath.Join(root, relativeRoot)
		err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("digest input is not regular: %s", path)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			rows = append(rows, hex.EncodeToString(sum[:])+"  "+filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			t.Fatalf("digest %s: %v", relativeRoot, err)
		}
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n") + "\n"
}

func writeEvidence(t *testing.T, name string, content []byte) {
	t.Helper()
	root := os.Getenv("FOUNDRY_SKILL_EVOLUTION_EVIDENCE")
	if root == "" {
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create evidence directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
		t.Fatalf("write evidence %s: %v", name, err)
	}
}
