package mockup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RetentionRoot = "data/visual-inputs"

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
	if err := os.MkdirAll(RetentionRoot, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: mkdir retention root: %w", err)
	}
	id := fmt.Sprintf("visual-%d", now.UTC().UnixNano())
	dir := filepath.Join(RetentionRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: mkdir artifact dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("mockup ingest: write content: %w", err)
	}
	artifact := Artifact{
		ID:        id,
		Name:      name,
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
