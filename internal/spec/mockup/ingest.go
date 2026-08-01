package mockup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// retentionRootOverride, when non-empty, is the directory Ingest writes under.
// Tests must set this to t.TempDir() via SetRetentionRoot; production callers
// rely on the default absolute path under data/visual-inputs.
var retentionRootOverride string

// SetRetentionRoot configures where Ingest stores visual-input artifacts.
// Only tests should call this — always pass t.TempDir() (docs/PLAN.md Task 131).
func SetRetentionRoot(root string) {
	retentionRootOverride = root
}

// RetentionRoot returns the absolute visual-inputs retention directory.
func RetentionRoot() string {
	if retentionRootOverride != "" {
		abs, err := filepath.Abs(retentionRootOverride)
		if err != nil {
			return retentionRootOverride
		}
		return abs
	}
	abs, err := filepath.Abs(filepath.Join("data", "visual-inputs"))
	if err != nil {
		return filepath.Join("data", "visual-inputs")
	}
	return abs
}

type Artifact struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

func Ingest(name, mediaType string, content []byte, now time.Time) (Artifact, error) {
	if strings.TrimSpace(name) == "" {
		return Artifact{}, fmt.Errorf("mockup ingest: name is required")
	}
	// Sanitize name to prevent path traversal (OWASP A03 / CWE-22).
	safeName := filepath.Base(filepath.Clean(name))
	if safeName == "" || safeName == "." || safeName == ".." {
		return Artifact{}, fmt.Errorf("mockup ingest: invalid name %q", name)
	}
	root := RetentionRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: mkdir retention root: %w", err)
	}
	id := fmt.Sprintf("visual-%d", now.UTC().UnixNano())
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: mkdir artifact dir: %w", err)
	}
	path := filepath.Join(dir, safeName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: write content: %w", err)
	}
	artifact := Artifact{
		ID:        id,
		Name:      safeName,
		MediaType: mediaType,
		Path:      path,
		CreatedAt: now.UTC(),
	}
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), raw, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: write metadata: %w", err)
	}
	return artifact, nil
}
