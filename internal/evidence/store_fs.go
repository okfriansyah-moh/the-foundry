package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrBundleExists is returned by FSStore.Put when a bundle with the computed
// ID already exists on disk. Bundles are immutable: a second Put for the
// same content is a no-op error, never a silent overwrite.
var ErrBundleExists = errors.New("evidence: bundle already exists")

// ErrVerifyFailed is returned by FSStore.Verify when a stored bundle does
// not match its own manifest — either the manifest digest no longer matches
// its content, or an artifact's on-disk bytes no longer match its recorded
// hash/size.
var ErrVerifyFailed = errors.New("evidence: verification failed")

// Store persists and retrieves evidence Bundles and can independently
// re-verify their integrity. FSStore and S3Store both satisfy this interface
// with identical content-addressing semantics.
type Store interface {
	// Put persists bundle and returns its content-derived ID. It errors
	// with ErrBundleExists if a bundle with that ID is already stored.
	Put(bundle Bundle) (string, error)
	// Get loads the bundle stored under id.
	Get(id string) (Bundle, error)
	// Verify re-hashes every artifact and the manifest itself from bytes
	// on disk and returns an error naming the first mismatch found. It
	// never trusts a previously stored hash as evidence of itself.
	Verify(id string) error
}

// FSStore is a Store backed by a content-addressed filesystem layout under
// Root: evidence/<sha[0:2]>/<sha>/{manifest.json,artifacts/...}.
type FSStore struct {
	// Root is the evidence directory, typically
	// $FOUNDRY_DATA_DIR/evidence.
	Root string
}

// NewFSStore returns an FSStore rooted at root. root is created on first
// Put if it does not already exist.
func NewFSStore(root string) *FSStore {
	return &FSStore{Root: root}
}

func (s *FSStore) bundleDir(id string) string {
	return filepath.Join(s.Root, id[:2], id)
}

// Put computes bundle.Manifest's digest, uses it as the bundle ID, and
// copies bundle.Dir's artifacts (as named by bundle.Manifest.Artifacts)
// into the content-addressed layout alongside a manifest.json. It errors
// with ErrBundleExists if the ID is already stored.
func (s *FSStore) Put(bundle Bundle) (string, error) {
	id, err := bundle.Manifest.DigestHex()
	if err != nil {
		return "", fmt.Errorf("evidence: compute bundle id: %w", err)
	}

	dir := s.bundleDir(id)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("%w: %s", ErrBundleExists, id)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("evidence: stat bundle dir %s: %w", dir, err)
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return "", fmt.Errorf("evidence: create bundle dir %s: %w", dir, err)
	}

	for _, ref := range bundle.Manifest.Artifacts {
		srcPath, err := safeJoin(bundle.Dir, ref.Path)
		if err != nil {
			return "", fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		dstPath, err := safeJoin(artifactsDir, ref.Path)
		if err != nil {
			return "", fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return "", fmt.Errorf("evidence: copy artifact %s: %w", ref.Path, err)
		}
	}

	canon, err := bundle.Manifest.canonicalJSON()
	if err != nil {
		return "", fmt.Errorf("evidence: encode manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), canon, 0o644); err != nil {
		return "", fmt.Errorf("evidence: write manifest: %w", err)
	}

	return id, nil
}

// Get loads the bundle stored under id.
func (s *FSStore) Get(id string) (Bundle, error) {
	dir := s.bundleDir(id)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Bundle{}, fmt.Errorf("evidence: read manifest for %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Bundle{}, fmt.Errorf("evidence: decode manifest for %s: %w", id, err)
	}
	return Bundle{Manifest: m, Dir: filepath.Join(dir, "artifacts")}, nil
}

// Verify re-derives every hash in the stored bundle id from bytes actually
// on disk: the manifest's own digest must equal id, and every artifact's
// current sha256/size must match its ArtifactRef. It never compares a
// stored hash to another stored hash.
func (s *FSStore) Verify(id string) error {
	dir := s.bundleDir(id)
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("evidence: read manifest for %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("evidence: decode manifest for %s: %w", id, err)
	}

	gotDigest, err := m.DigestHex()
	if err != nil {
		return fmt.Errorf("evidence: recompute manifest digest for %s: %w", id, err)
	}
	if gotDigest != id {
		return fmt.Errorf("%w: manifest.json digest %s does not match bundle id %s", ErrVerifyFailed, gotDigest, id)
	}

	artifactsDir := filepath.Join(dir, "artifacts")
	for _, ref := range m.Artifacts {
		path, err := safeJoin(artifactsDir, ref.Path)
		if err != nil {
			return fmt.Errorf("%w: artifact %s: %v", ErrVerifyFailed, ref.Path, err)
		}
		sum, size, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("%w: artifact %s: %v", ErrVerifyFailed, ref.Path, err)
		}
		if sum != ref.SHA256 {
			return fmt.Errorf("%w: artifact %s: sha256 mismatch (stored %s, actual %s)", ErrVerifyFailed, ref.Path, ref.SHA256, sum)
		}
		if size != ref.Bytes {
			return fmt.Errorf("%w: artifact %s: size mismatch (stored %d, actual %d)", ErrVerifyFailed, ref.Path, ref.Bytes, size)
		}
	}
	return nil
}

// safeJoin joins base and rel, rejecting any rel that would escape base
// (e.g. via ".." segments or an absolute path) — evidence artifact paths
// are untrusted input that must never resolve outside the content-addressed
// bundle directory.
func safeJoin(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifact path must be relative: %s", rel)
	}
	joined := filepath.Join(base, rel)
	cleanBase := filepath.Clean(base) + string(filepath.Separator)
	if joined != filepath.Clean(base) && !strings.HasPrefix(joined, cleanBase) {
		return "", fmt.Errorf("artifact path escapes bundle directory: %s", rel)
	}
	return joined, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", dst, err)
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}
