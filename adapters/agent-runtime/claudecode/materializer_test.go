package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
)

func TestInstallDoctorAndIdempotence(t *testing.T) {
	workspace := t.TempDir()
	set := agentSet("planning", "reviewer")
	materializer := Materializer{}
	first, err := materializer.Install(context.Background(), workspace, set)
	if err != nil {
		t.Fatal(err)
	}
	before := workspaceDigests(t, workspace)
	second, err := materializer.Install(context.Background(), workspace, set)
	if err != nil {
		t.Fatal(err)
	}
	after := workspaceDigests(t, workspace)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("second install changed bytes:\nbefore=%v\nafter=%v", before, after)
	}
	if first != second {
		t.Fatalf("results differ: first=%+v second=%+v", first, second)
	}
	if _, err := materializer.Doctor(context.Background(), workspace, set); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
}

func TestDoctorDetectsMissingAndTamperedFiles(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(string) error
	}{
		{name: "missing", change: os.Remove},
		{name: "tampered", change: func(path string) error { return os.WriteFile(path, []byte("tampered"), 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			set := agentSet("planning")
			if _, err := (Materializer{}).Install(context.Background(), workspace, set); err != nil {
				t.Fatal(err)
			}
			if err := test.change(filepath.Join(workspace, ".claude/agents/planning.md")); err != nil {
				t.Fatal(err)
			}
			if _, err := (Materializer{}).Doctor(context.Background(), workspace, set); err == nil {
				t.Fatal("Doctor succeeded after installed file changed")
			}
		})
	}
}

func TestInstallLeavesPreviouslyInstalledDisabledPackagesUntouched(t *testing.T) {
	workspace := t.TempDir()
	materializer := Materializer{}
	initial := skillSet("guardrails", "testing")
	if _, err := materializer.Install(context.Background(), workspace, initial); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(workspace, ".claude/skills/testing/SKILL.md")
	before, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails")); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want fail-closed manifest collision", err)
	}
	after, err := os.ReadFile(stale)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("disabled skill changed: content=%q err=%v", after, err)
	}

	workspace = t.TempDir()
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".claude/skills/testing/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("fresh install materialized disabled skill: %v", err)
	}
}

func TestInstallRejectsSymlinkEscapeAndCollision(t *testing.T) {
	t.Run("symlink escape", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, ".claude")); err != nil {
			t.Fatal(err)
		}
		if _, err := (Materializer{}).Install(context.Background(), workspace, agentSet("planning")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Install error = %v, want symlink rejection", err)
		}
		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Fatalf("outside directory changed: entries=%v err=%v", entries, err)
		}
	})
	t.Run("unowned collision", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, ".claude/agents/planning.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user file"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (Materializer{}).Install(context.Background(), workspace, agentSet("planning")); err == nil || !strings.Contains(err.Error(), "collision") {
			t.Fatalf("Install error = %v, want collision rejection", err)
		}
	})
}

func TestKindsHaveIndependentManifestsAndDoNotMutateConfig(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, ".foundry/config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("executor_allowlist:\n  - claude-code\n")
	if err := os.WriteFile(config, original, 0o600); err != nil {
		t.Fatal(err)
	}
	materializer := Materializer{}
	for _, set := range []agentruntime.PackageSet{agentSet("planning"), skillSet("guardrails")} {
		if _, err := materializer.Install(context.Background(), workspace, set); err != nil {
			t.Fatal(err)
		}
	}
	for _, manifest := range []string{"agents.json", "skills.json"} {
		if _, err := os.Stat(filepath.Join(workspace, ".foundry/agent-runtime/claude-code", manifest)); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(config)
	if err != nil || !reflect.DeepEqual(actual, original) {
		t.Fatalf("config mutated: got=%q err=%v", actual, err)
	}
}

func TestTamperedManifestCannotClaimFilesOutsideProviderNamespace(t *testing.T) {
	workspace := t.TempDir()
	materializer := Materializer{}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails", "testing")); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(workspace, "README.md")
	content := []byte("user-owned\n")
	if err := os.WriteFile(readme, content, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(workspace, ".foundry/agent-runtime/claude-code/skills.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var ownership manifest
	if err := json.Unmarshal(raw, &ownership); err != nil {
		t.Fatal(err)
	}
	ownership.Files = append(ownership.Files, manifestFile{Path: "README.md", Digest: digestBytes(content)})
	raw, err = json.MarshalIndent(ownership, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails", "testing")); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want forged manifest collision", err)
	}
	actual, err := os.ReadFile(readme)
	if err != nil || !reflect.DeepEqual(actual, content) {
		t.Fatalf("user file changed: content=%q err=%v", actual, err)
	}
}

func TestForgedValidManifestCannotAuthorizeDeletion(t *testing.T) {
	workspace := t.TempDir()
	materializer := Materializer{}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails")); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(workspace, ".claude/skills/forged/user.txt")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("user-owned\n")
	if err := os.WriteFile(userPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(workspace, ".foundry/agent-runtime/claude-code/skills.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var forged manifest
	if err := json.Unmarshal(raw, &forged); err != nil {
		t.Fatal(err)
	}
	forged.Files = append(forged.Files, manifestFile{Path: ".claude/skills/forged/user.txt", Digest: digestBytes(content)})
	raw, err = json.MarshalIndent(forged, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails")); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want forged manifest collision", err)
	}
	actual, err := os.ReadFile(userPath)
	if err != nil || !reflect.DeepEqual(actual, content) {
		t.Fatalf("manifest-authorized user file changed: content=%q err=%v", actual, err)
	}
}

func TestForgedValidManifestCannotAuthorizeOverwrite(t *testing.T) {
	workspace := t.TempDir()
	materializer := Materializer{}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails")); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(workspace, ".claude/skills/testing/SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("user-owned\n")
	if err := os.WriteFile(userPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(workspace, ".foundry/agent-runtime/claude-code/skills.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var forged manifest
	if err := json.Unmarshal(raw, &forged); err != nil {
		t.Fatal(err)
	}
	forged.Files = append(forged.Files, manifestFile{Path: ".claude/skills/testing/SKILL.md", Digest: digestBytes(content)})
	raw, err = json.MarshalIndent(forged, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Install(context.Background(), workspace, skillSet("guardrails", "testing")); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want user-owned destination collision", err)
	}
	actual, err := os.ReadFile(userPath)
	if err != nil || !reflect.DeepEqual(actual, content) {
		t.Fatalf("manifest-authorized overwrite changed user file: content=%q err=%v", actual, err)
	}
}

func TestInstallFailsClosedWhenCatalogPinChanges(t *testing.T) {
	workspace := t.TempDir()
	materializer := Materializer{}
	set := skillSet("guardrails")
	if _, err := materializer.Install(context.Background(), workspace, set); err != nil {
		t.Fatal(err)
	}
	set.CatalogDigest = "sha256:new-catalog"
	if _, err := materializer.Install(context.Background(), workspace, set); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want changed manifest collision", err)
	}
}

func TestInstallDirectorySwapCannotEscapeWorkspaceRoot(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	sentinelPath := filepath.Join(outside, "sentinel.txt")
	sentinel := []byte("outside-owned\n")
	if err := os.WriteFile(sentinelPath, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	var swapErr error
	swapped := false
	materializer := Materializer{beforeCommit: func(relative string) {
		if swapped || relative != ".claude/agents/planning.md" {
			return
		}
		swapped = true
		agents := filepath.Join(workspace, ".claude/agents")
		swapErr = os.Rename(agents, agents+"-held")
		if swapErr == nil {
			swapErr = os.Symlink(outside, agents)
		}
	}}
	if _, err := materializer.Install(context.Background(), workspace, agentSet("planning")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Install error = %v, want symlink race rejection", err)
	}
	if swapErr != nil {
		t.Fatal(swapErr)
	}
	actual, err := os.ReadFile(sentinelPath)
	if err != nil || !reflect.DeepEqual(actual, sentinel) {
		t.Fatalf("outside sentinel changed: content=%q err=%v", actual, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel.txt" {
		t.Fatalf("outside directory changed: entries=%v err=%v", entries, err)
	}
}

func TestInstallDoesNotOverwriteConcurrentRegularFileChange(t *testing.T) {
	workspace := t.TempDir()
	set := agentSet("planning")
	if _, err := (Materializer{}).Install(context.Background(), workspace, set); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, ".claude/agents/planning.md")
	concurrent := []byte("concurrent user bytes\n")
	changed := false
	materializer := Materializer{beforeCommit: func(relative string) {
		if changed || relative != ".claude/agents/planning.md" {
			return
		}
		changed = true
		if err := os.WriteFile(target, concurrent, 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := materializer.Install(context.Background(), workspace, set); err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("Install error = %v, want concurrent destination collision", err)
	}
	actual, err := os.ReadFile(target)
	if err != nil || !reflect.DeepEqual(actual, concurrent) {
		t.Fatalf("concurrent bytes changed: content=%q err=%v", actual, err)
	}
}

func TestProjectionRejectsTraversalAndPathCollision(t *testing.T) {
	set := skillSet("guardrails")
	set.Packages[0].References = []agentruntime.File{{Path: "../escape", Bytes: []byte("bad")}}
	if _, err := (Materializer{}).Install(context.Background(), t.TempDir(), set); err == nil || !strings.Contains(err.Error(), "unsafe reference") {
		t.Fatalf("Install error = %v, want traversal rejection", err)
	}
	set = skillSet("guardrails")
	set.Packages[0].References = []agentruntime.File{{Path: "SKILL.md", Bytes: []byte("collision")}}
	if _, err := (Materializer{}).Install(context.Background(), t.TempDir(), set); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("Install error = %v, want collision rejection", err)
	}
}

func agentSet(names ...string) agentruntime.PackageSet {
	set := agentruntime.PackageSet{Kind: agentruntime.KindAgents, CatalogDigest: "sha256:catalog", EnablementDigest: "sha256:enabled"}
	for _, name := range names {
		set.Packages = append(set.Packages, agentruntime.Package{Name: name, Description: name + " agent", Skills: []string{"guardrails"}, Source: []byte("# " + name + "\n")})
	}
	return set
}

func skillSet(names ...string) agentruntime.PackageSet {
	set := agentruntime.PackageSet{Kind: agentruntime.KindSkills, CatalogDigest: "sha256:catalog", EnablementDigest: "sha256:enabled"}
	for _, name := range names {
		set.Packages = append(set.Packages, agentruntime.Package{Name: name, Source: []byte("---\nname: " + name + "\n---\n")})
	}
	return set
}

func workspaceDigests(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
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
		result[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
