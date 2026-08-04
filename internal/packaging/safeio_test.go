package packaging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCanonicalFileRejectsDeterministicParentDirectorySwap(t *testing.T) {
	root := t.TempDir()
	originalDir := filepath.Join(root, "skills/example")
	if err := os.MkdirAll(originalDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originalDir, "SKILL.md"), []byte("trusted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideBytes := []byte("outside attacker bytes\n")
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), outsideBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	swapped := false
	content, err := readCanonicalFileWithHook(root, "skills/example/SKILL.md", func(stage, path string) error {
		if swapped || stage != "after-parent-lstat" || path != "skills/example" {
			return nil
		}
		swapped = true
		if err := os.Rename(originalDir, filepath.Join(root, "skills/original")); err != nil {
			return err
		}
		return os.Symlink(outside, originalDir)
	})
	if !swapped {
		t.Fatal("race hook did not swap the selected parent")
	}
	if err == nil {
		t.Fatalf("readCanonicalFileWithHook returned %q after directory swap", content)
	}
	if bytes.Equal(content, outsideBytes) || strings.Contains(string(content), "attacker") {
		t.Fatalf("outside bytes were read: %q", content)
	}
}

func TestReadCanonicalFileRejectsDeterministicFinalFileSwap(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "skills/example")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(path, []byte("trusted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement.md")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := readCanonicalFileWithHook(root, "skills/example/SKILL.md", func(stage, _ string) error {
		if stage != "after-file-lstat" {
			return nil
		}
		if err := os.Rename(path, filepath.Join(directory, "original.md")); err != nil {
			return err
		}
		return os.Rename(replacement, path)
	})
	if err == nil {
		t.Fatalf("readCanonicalFileWithHook returned %q after file swap", content)
	}
}
