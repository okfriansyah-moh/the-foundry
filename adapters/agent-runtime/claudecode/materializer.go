package claudecode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
)

const (
	providerName   = "claude-code"
	manifestFormat = 1
)

var packageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Materializer writes Claude Code packages beneath one workspace.
type Materializer struct {
	beforeCommit func(string)
}

type manifest struct {
	Version          int               `json:"version"`
	Provider         string            `json:"provider"`
	Kind             agentruntime.Kind `json:"kind"`
	CatalogDigest    string            `json:"catalog_digest"`
	EnablementDigest string            `json:"enablement_digest"`
	Files            []manifestFile    `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type projectedFile struct {
	path    string
	content []byte
	digest  string
}

// Install writes the enabled projection. Files left by packages that have since
// been disabled are not removed because the workspace manifest is not deletion
// authority.
func (m Materializer) Install(ctx context.Context, workspace string, set agentruntime.PackageSet) (agentruntime.Result, error) {
	root, err := openWorkspaceRoot(workspace)
	if err != nil {
		return agentruntime.Result{}, err
	}
	defer root.close()
	files, expectedManifest, manifestBytes, err := projection(set)
	if err != nil {
		return agentruntime.Result{}, err
	}
	manifestPath := manifestRelativePath(set.Kind)
	if err := ctx.Err(); err != nil {
		return agentruntime.Result{}, err
	}
	for _, file := range files {
		if err := preflightDestination(root, file.path, file.content); err != nil {
			return agentruntime.Result{}, err
		}
	}
	if err := preflightDestination(root, manifestPath, manifestBytes); err != nil {
		return agentruntime.Result{}, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return agentruntime.Result{}, err
		}
		if err := root.writeFile(file.path, file.content, m.beforeCommit); err != nil {
			return agentruntime.Result{}, err
		}
	}
	if err := root.writeFile(manifestPath, manifestBytes, m.beforeCommit); err != nil {
		return agentruntime.Result{}, err
	}
	return result(set.Kind, len(expectedManifest.Files), manifestBytes), nil
}

// Doctor verifies the ownership manifest and every installed byte against the
// current validated catalog and enablement inputs.
func (Materializer) Doctor(ctx context.Context, workspace string, set agentruntime.PackageSet) (agentruntime.Result, error) {
	root, err := openWorkspaceRoot(workspace)
	if err != nil {
		return agentruntime.Result{}, err
	}
	defer root.close()
	files, _, manifestBytes, err := projection(set)
	if err != nil {
		return agentruntime.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.Result{}, err
	}
	actualManifest, err := root.readFile(manifestRelativePath(set.Kind))
	if err != nil {
		return agentruntime.Result{}, fmt.Errorf("claude-code: doctor manifest: %w", err)
	}
	if !bytes.Equal(actualManifest, manifestBytes) {
		return agentruntime.Result{}, fmt.Errorf("claude-code: doctor manifest does not match catalog and enablement pins")
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return agentruntime.Result{}, err
		}
		actual, err := root.readFile(file.path)
		if err != nil {
			return agentruntime.Result{}, fmt.Errorf("claude-code: doctor %s: %w", file.path, err)
		}
		if !bytes.Equal(actual, file.content) {
			return agentruntime.Result{}, fmt.Errorf("claude-code: doctor %s digest mismatch", file.path)
		}
	}
	return result(set.Kind, len(files), manifestBytes), nil
}

func projection(set agentruntime.PackageSet) ([]projectedFile, manifest, []byte, error) {
	if set.Kind != agentruntime.KindAgents && set.Kind != agentruntime.KindSkills {
		return nil, manifest{}, nil, fmt.Errorf("claude-code: unsupported kind %q", set.Kind)
	}
	if set.CatalogDigest == "" || set.EnablementDigest == "" {
		return nil, manifest{}, nil, fmt.Errorf("claude-code: catalog and enablement digests are required")
	}
	byPath := make(map[string][]byte)
	seenPackages := make(map[string]struct{}, len(set.Packages))
	for _, pkg := range set.Packages {
		if !packageNamePattern.MatchString(pkg.Name) {
			return nil, manifest{}, nil, fmt.Errorf("claude-code: invalid package name %q", pkg.Name)
		}
		if _, duplicate := seenPackages[pkg.Name]; duplicate {
			return nil, manifest{}, nil, fmt.Errorf("claude-code: duplicate package %q", pkg.Name)
		}
		seenPackages[pkg.Name] = struct{}{}
		if set.Kind == agentruntime.KindAgents {
			path := filepath.ToSlash(filepath.Join(".claude", "agents", pkg.Name+".md"))
			byPath[path] = renderAgent(pkg)
			continue
		}
		base := filepath.ToSlash(filepath.Join(".claude", "skills", pkg.Name))
		byPath[base+"/SKILL.md"] = append([]byte(nil), pkg.Source...)
		for _, reference := range pkg.References {
			if !safeRelative(reference.Path) {
				return nil, manifest{}, nil, fmt.Errorf("claude-code: package %q has unsafe reference %q", pkg.Name, reference.Path)
			}
			path := base + "/" + filepath.ToSlash(filepath.Clean(reference.Path))
			if _, collision := byPath[path]; collision {
				return nil, manifest{}, nil, fmt.Errorf("claude-code: projection path collision at %q", path)
			}
			byPath[path] = append([]byte(nil), reference.Bytes...)
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]projectedFile, 0, len(paths))
	m := manifest{Version: manifestFormat, Provider: providerName, Kind: set.Kind, CatalogDigest: set.CatalogDigest, EnablementDigest: set.EnablementDigest}
	for _, path := range paths {
		digest := digestBytes(byPath[path])
		files = append(files, projectedFile{path: path, content: byPath[path], digest: digest})
		m.Files = append(m.Files, manifestFile{Path: path, Digest: digest})
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, manifest{}, nil, fmt.Errorf("claude-code: encode ownership manifest: %w", err)
	}
	raw = append(raw, '\n')
	return files, m, raw, nil
}

func renderAgent(pkg agentruntime.Package) []byte {
	var output bytes.Buffer
	output.WriteString("---\nname: ")
	name, _ := json.Marshal(pkg.Name)
	output.Write(name)
	output.WriteString("\ndescription: ")
	description, _ := json.Marshal(pkg.Description)
	output.Write(description)
	if len(pkg.Skills) > 0 {
		output.WriteString("\nskills:")
		for _, skill := range pkg.Skills {
			encoded, _ := json.Marshal(skill)
			output.WriteString("\n  - ")
			output.Write(encoded)
		}
	}
	output.WriteString("\n---\n\n")
	output.Write(pkg.Source)
	return output.Bytes()
}

func preflightDestination(root *workspaceRoot, relative string, expected []byte) error {
	actual, err := root.readFile(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("claude-code: inspect destination %s: %w", relative, err)
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("claude-code: destination collision at %s", relative)
	}
	return nil
}

type workspaceRoot struct {
	root *os.Root
}

func openWorkspaceRoot(workspace string) (*workspaceRoot, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("claude-code: resolve workspace: %w", err)
	}
	before, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("claude-code: inspect workspace: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, fmt.Errorf("claude-code: workspace must be a real directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("claude-code: workspace must be a real directory: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("claude-code: workspace changed while opening")
	}
	return &workspaceRoot{root: root}, nil
}

func (r *workspaceRoot) close() {
	_ = r.root.Close()
}

func cleanWorkspacePath(relative string) (string, error) {
	if !safeRelative(relative) {
		return "", fmt.Errorf("unsafe workspace path %q", relative)
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))), nil
}

func (r *workspaceRoot) readFile(relative string) ([]byte, error) {
	clean, info, err := r.inspectPath(relative)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, os.ErrNotExist
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", relative)
	}
	return r.root.ReadFile(clean)
}

func (r *workspaceRoot) writeFile(relative string, content []byte, beforeCommit func(string)) error {
	clean, _, err := r.inspectPath(relative)
	if err != nil {
		return fmt.Errorf("claude-code: write %s: %w", relative, err)
	}
	if err := r.makeDirs(filepath.ToSlash(filepath.Dir(clean))); err != nil {
		return fmt.Errorf("claude-code: write %s: %w", relative, err)
	}
	if beforeCommit != nil {
		beforeCommit(relative)
	}
	exists, err := r.destinationMatches(relative, content)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	file, err := r.root.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		exists, matchErr := r.destinationMatches(relative, content)
		if matchErr != nil {
			return matchErr
		}
		if exists {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("claude-code: create %s: %w", relative, err)
	}
	created, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return fmt.Errorf("claude-code: inspect created %s: %w", relative, statErr)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		r.removeCreatedFile(clean, created)
		return fmt.Errorf("claude-code: write %s: %w", relative, err)
	}
	if err := file.Close(); err != nil {
		r.removeCreatedFile(clean, created)
		return fmt.Errorf("claude-code: close %s: %w", relative, err)
	}
	actual, err := r.readFile(relative)
	if err != nil {
		return fmt.Errorf("claude-code: verify write %s: %w", relative, err)
	}
	if !bytes.Equal(actual, content) {
		return fmt.Errorf("claude-code: verify write %s: content mismatch", relative)
	}
	return nil
}

func (r *workspaceRoot) destinationMatches(relative string, expected []byte) (bool, error) {
	actual, err := r.readFile(relative)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claude-code: inspect destination %s: %w", relative, err)
	}
	if !bytes.Equal(actual, expected) {
		return false, fmt.Errorf("claude-code: destination collision at %s", relative)
	}
	return true, nil
}

func (r *workspaceRoot) removeCreatedFile(relative string, created os.FileInfo) {
	current, err := r.root.Lstat(relative)
	if err == nil && current.Mode().IsRegular() && os.SameFile(current, created) {
		_ = r.root.Remove(relative)
	}
}

func (r *workspaceRoot) inspectPath(relative string) (string, os.FileInfo, error) {
	clean, err := cleanWorkspacePath(relative)
	if err != nil {
		return "", nil, err
	}
	parts := strings.Split(clean, "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")
		info, err := r.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return clean, nil, nil
		}
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("workspace path %q contains a symlink", relative)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", nil, fmt.Errorf("workspace path %q has a non-directory parent", relative)
		}
		if index == len(parts)-1 {
			return clean, info, nil
		}
	}
	return clean, nil, nil
}

func (r *workspaceRoot) makeDirs(relative string) error {
	if relative == "." || relative == "" {
		return nil
	}
	clean, err := cleanWorkspacePath(relative)
	if err != nil {
		return err
	}
	parts := strings.Split(clean, "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")
		info, err := r.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := r.root.Mkdir(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("directory path contains symlink or non-directory")
		}
	}
	return nil
}

func manifestRelativePath(kind agentruntime.Kind) string {
	return filepath.ToSlash(filepath.Join(".foundry", "agent-runtime", providerName, string(kind)+".json"))
}

func safeRelative(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	return clean != "." && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func result(kind agentruntime.Kind, files int, manifestBytes []byte) agentruntime.Result {
	return agentruntime.Result{Provider: providerName, Kind: kind, Files: files, ManifestDigest: digestBytes(manifestBytes)}
}

var _ agentruntime.Materializer = Materializer{}
