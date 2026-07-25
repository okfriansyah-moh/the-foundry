package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildStagedBundle writes one artifact file into a fresh staging dir and
// returns a Bundle whose Manifest.Artifacts correctly describes it.
func buildStagedBundle(t *testing.T, content []byte) Bundle {
	t.Helper()
	stageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stageDir, "output.txt"), content, 0o644); err != nil {
		t.Fatalf("write staged artifact: %v", err)
	}
	sum := sha256.Sum256(content)

	return Bundle{
		Manifest: Manifest{
			WorkflowID: "wf-1",
			TaskID:     "task-11",
			Artifacts: []ArtifactRef{
				{Path: "output.txt", SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(content))},
			},
			CreatedAt: time.Unix(0, 0).UTC(),
		},
		Dir: stageDir,
	}
}

func TestFSStorePutGetVerifyRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewFSStore(root)

	bundle := buildStagedBundle(t, []byte("hello"))
	id, err := store.Put(bundle)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Manifest.TaskID != "task-11" {
		t.Errorf("TaskID = %q, want task-11", got.Manifest.TaskID)
	}

	if err := store.Verify(id); err != nil {
		t.Errorf("Verify on untampered bundle: %v", err)
	}
}

func TestFSStorePutSecondTimeErrors(t *testing.T) {
	root := t.TempDir()
	store := NewFSStore(root)

	bundle := buildStagedBundle(t, []byte("hello"))
	id, err := store.Put(bundle)
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}

	bundle2 := buildStagedBundle(t, []byte("hello"))
	if _, err := store.Put(bundle2); err == nil {
		t.Fatalf("second Put with same content (id %s) succeeded, want ErrBundleExists", id)
	}
}

func TestFSStoreVerifyDetectsBitFlip(t *testing.T) {
	root := t.TempDir()
	store := NewFSStore(root)

	bundle := buildStagedBundle(t, []byte("hello"))
	id, err := store.Put(bundle)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	artifactPath := filepath.Join(root, id[:2], id, "artifacts", "output.txt")
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read stored artifact: %v", err)
	}
	raw[0] ^= 0xFF // flip one byte
	if err := os.WriteFile(artifactPath, raw, 0o644); err != nil {
		t.Fatalf("rewrite tampered artifact: %v", err)
	}

	err = store.Verify(id)
	if err == nil {
		t.Fatalf("Verify succeeded after bit-flip in %s, want failure", artifactPath)
	}
	if !containsAll(err.Error(), "output.txt", "sha256 mismatch") {
		t.Errorf("Verify error does not name the tampered file: %v", err)
	}
}

func TestFSStoreVerifyDetectsManifestTamper(t *testing.T) {
	root := t.TempDir()
	store := NewFSStore(root)

	bundle := buildStagedBundle(t, []byte("hello"))
	id, err := store.Put(bundle)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	manifestPath := filepath.Join(root, id[:2], id, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	tampered := append(raw[:len(raw)-1], []byte(`"x"}`)...)
	if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
		t.Fatalf("rewrite tampered manifest: %v", err)
	}

	if err := store.Verify(id); err == nil {
		t.Fatalf("Verify succeeded after manifest tamper, want failure")
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	if _, err := safeJoin(base, "../escape.txt"); err == nil {
		t.Errorf("safeJoin allowed a path escaping base")
	}
	if _, err := safeJoin(base, "/etc/passwd"); err == nil {
		t.Errorf("safeJoin allowed an absolute path")
	}
	if _, err := safeJoin(base, "sub/dir/file.txt"); err != nil {
		t.Errorf("safeJoin rejected a legitimate relative path: %v", err)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
