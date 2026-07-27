package pdp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// loadRegoFiles reads every *.rego file directly inside dir — no
// subdirectories, no symlink traversal — and returns them keyed by
// filename. os.ReadDir never returns an entry whose Name contains a path
// separator or "..", but this function does not rely on that invariant
// silently: any suspicious entry name is rejected explicitly, and every
// resolved path is verified to stay inside dir before it is read.
func loadRegoFiles(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("pdp: read rego bundle dir %q: %w", dir, err)
	}

	cleanDir := filepath.Clean(dir)
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".rego") {
			continue
		}
		if entry.IsDir() || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
			return nil, fmt.Errorf("pdp: rego bundle dir %q: refusing suspicious entry %q", dir, name)
		}

		path := filepath.Join(cleanDir, name)
		if !strings.HasPrefix(path, cleanDir+string(filepath.Separator)) {
			return nil, fmt.Errorf("pdp: rego bundle dir %q: entry %q escapes bundle root", dir, name)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pdp: read rego file %q: %w", path, err)
		}
		files[name] = string(content)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("pdp: rego bundle dir %q: no .rego files found", dir)
	}
	return files, nil
}

// digestFiles computes a sha256 digest over files, walked in sorted
// filename order so the result never depends on directory-listing order.
func digestFiles(files map[string]string) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\n%s\n", name, files[name])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// BundleDigest computes the digest NewOPADecider will require as
// pinnedBundleDigest for the rego files in dir, without constructing a
// Decider. Callers use this once, after an intentional rego change, to
// produce the value they then pin (e.g. in their own boot configuration).
func BundleDigest(dir string) (string, error) {
	files, err := loadRegoFiles(dir)
	if err != nil {
		return "", err
	}
	return digestFiles(files), nil
}
