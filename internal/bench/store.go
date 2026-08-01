package bench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultTargetsPath is the V1 acceptance threshold config.
	DefaultTargetsPath = "config/benchmark-targets.yaml"
	// DefaultBaselineDir is the evidence namespace for control-arm runs.
	DefaultBaselineDir = "benchmarks/baseline"
)

// Targets holds V1 acceptance targets (not universal claims).
type Targets struct {
	Version string       `yaml:"version"`
	Label   string       `yaml:"label"`
	Personal ArmTargets `yaml:"personal"`
	TenX     ArmTargets `yaml:"tenx"`
	Quality  QualityGate `yaml:"quality_gate"`
}

// ArmTargets are per-path V1 acceptance thresholds.
type ArmTargets struct {
	ManualOrchestrationReduction   float64 `yaml:"manual_orchestration_reduction"`
	DeliveryLeadTimeReduction      float64 `yaml:"delivery_lead_time_reduction"`
	PlanToHandoffReduction         float64 `yaml:"plan_to_handoff_reduction"`
	CoordinationReportingReduction float64 `yaml:"coordination_reporting_reduction"`
	UnauthorizedActionsMax         float64 `yaml:"unauthorized_actions_max"`
	UnauthorizedSCMOperationsMax     float64 `yaml:"unauthorized_scm_operations_max"`
}

// QualityGate defines operational "quality no worse than baseline".
type QualityGate struct {
	Description              string `yaml:"description"`
	MaxDefectRegressionRatio float64 `yaml:"max_defect_regression_ratio"`
	MaxEvidenceRejectionIncrease float64 `yaml:"max_evidence_rejection_increase"`
}

// LoadTargets reads and validates benchmark-targets.yaml.
func LoadTargets(path string) (Targets, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultTargetsPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Targets{}, fmt.Errorf("bench: read targets %s: %w", path, err)
	}
	var t Targets
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return Targets{}, fmt.Errorf("bench: decode targets %s: %w", path, err)
	}
	if strings.TrimSpace(t.Version) == "" {
		return Targets{}, fmt.Errorf("bench: targets version is required")
	}
	if strings.TrimSpace(t.Label) == "" {
		return Targets{}, fmt.Errorf("bench: targets label is required")
	}
	return t, nil
}

// DeliverySpec names one comparable prior delivery to mine from git.
type DeliverySpec struct {
	ID       string `yaml:"id"`
	MergeRef string `yaml:"merge_ref"`
	Title    string `yaml:"title"`
}

// BaselineManifest lists control-arm deliveries and human-reported inputs.
type BaselineManifest struct {
	Version    string                  `yaml:"version"`
	Deliveries []DeliverySpec          `yaml:"deliveries"`
	HumanInput map[string]HumanInput   `yaml:"human_input"`
}

// LoadBaselineManifest reads benchmarks/baseline/manifest.yaml.
func LoadBaselineManifest(dir string) (BaselineManifest, error) {
	if dir == "" {
		dir = DefaultBaselineDir
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return BaselineManifest{}, fmt.Errorf("bench: read baseline manifest: %w", err)
	}
	var m BaselineManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return BaselineManifest{}, fmt.Errorf("bench: decode baseline manifest: %w", err)
	}
	if len(m.Deliveries) < 3 {
		return BaselineManifest{}, fmt.Errorf("bench: baseline manifest needs ≥3 deliveries, got %d", len(m.Deliveries))
	}
	return m, nil
}

// FileStore persists RunRecords as JSON under a directory.
type FileStore struct {
	Root string
}

// NewFileStore returns a file-backed store rooted at dir.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Root: dir}
}

func (s *FileStore) recordPath(id string) string {
	return filepath.Join(s.Root, id+".json")
}

// Save writes record as JSON (atomic replace).
func (s *FileStore) Save(record *RunRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return fmt.Errorf("bench: mkdir %s: %w", s.Root, err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("bench: marshal record: %w", err)
	}
	data = append(data, '\n')
	tmp := s.recordPath(record.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("bench: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.recordPath(record.ID)); err != nil {
		return fmt.Errorf("bench: rename record: %w", err)
	}
	return nil
}

// Load reads one record by id.
func (s *FileStore) Load(id string) (*RunRecord, error) {
	data, err := os.ReadFile(s.recordPath(id))
	if err != nil {
		return nil, fmt.Errorf("bench: read record %s: %w", id, err)
	}
	var r RunRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("bench: decode record %s: %w", id, err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// List returns all record IDs in stable sorted order.
func (s *FileStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("bench: list %s: %w", s.Root, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		// Skip aggregate/manifest sidecars that are not RunRecord files.
		if name == "summary.json" || name == "manifest.json" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}

// LoadAll reads every JSON record in the store.
func (s *FileStore) LoadAll() ([]*RunRecord, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}
	out := make([]*RunRecord, 0, len(ids))
	for _, id := range ids {
		r, err := s.Load(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// EnvironmentDigest hashes repo identity inputs so runs are comparable.
func EnvironmentDigest(repoRoot string) (string, error) {
	h := sha256.New()
	head, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	fmt.Fprintf(h, "head=%s\n", strings.TrimSpace(head))
	for _, rel := range []string{"go.mod", "config/benchmark-targets.yaml"} {
		p := filepath.Join(repoRoot, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("bench: digest %s: %w", rel, err)
		}
		fmt.Fprintf(h, "%s\n", rel)
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CaptureBaseline mines control-arm records from git per manifest and saves them.
func CaptureBaseline(ctx context.Context, repoRoot, baselineDir string) ([]*RunRecord, error) {
	_ = ctx
	manifest, err := LoadBaselineManifest(baselineDir)
	if err != nil {
		return nil, err
	}
	digest, err := EnvironmentDigest(repoRoot)
	if err != nil {
		return nil, err
	}
	store := NewFileStore(baselineDir)
	var records []*RunRecord
	for _, d := range manifest.Deliveries {
		g, defects, defectNote, err := MineDelivery(repoRoot, d.MergeRef)
		if err != nil {
			return nil, fmt.Errorf("bench: mine %s: %w", d.ID, err)
		}
		rec := NewRunRecord(d.ID, ArmControl, d.ID, d.Title, digest)
		if err := rec.ApplyGitDelivery(g, defects, defectNote); err != nil {
			return nil, err
		}
		if hi, ok := manifest.HumanInput[d.ID]; ok {
			if err := rec.ApplyHumanInput(hi); err != nil {
				return nil, err
			}
		}
		if err := store.Save(rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	summaryPath := filepath.Join(baselineDir, "summary.json")
	if err := writeSummary(summaryPath, records); err != nil {
		return nil, err
	}
	return records, nil
}

func writeSummary(path string, records []*RunRecord) error {
	type summary struct {
		CapturedAt time.Time    `json:"captured_at"`
		Arm        Arm          `json:"arm"`
		Count      int          `json:"count"`
		RecordIDs  []string     `json:"record_ids"`
		Records    []*RunRecord `json:"records"`
	}
	ids := make([]string, len(records))
	for i, r := range records {
		ids[i] = r.ID
	}
	data, err := json.MarshalIndent(summary{
		CapturedAt: time.Now().UTC(),
		Arm:        ArmControl,
		Count:      len(records),
		RecordIDs:  ids,
		Records:    records,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("bench: marshal summary: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// MineDelivery extracts git timing and proxy defect count for a merged delivery.
func MineDelivery(repoRoot, mergeRef string) (GitProvenance, float64, string, error) {
	mergeRef = strings.TrimSpace(mergeRef)
	if mergeRef == "" {
		return GitProvenance{}, 0, "", fmt.Errorf("merge ref is required")
	}
	parent1, err := gitOutput(repoRoot, "rev-parse", mergeRef+"^1")
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	parent2, err := gitOutput(repoRoot, "rev-parse", mergeRef+"^2")
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	parent1 = strings.TrimSpace(parent1)
	parent2 = strings.TrimSpace(parent2)
	base, err := gitOutput(repoRoot, "merge-base", parent1, parent2)
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	base = strings.TrimSpace(base)
	firstLine, err := gitOutput(repoRoot, "log", "--format=%H %cI %s", base+".."+parent2, "--reverse")
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return GitProvenance{}, 0, "", fmt.Errorf("no commits on branch for merge %s", mergeRef)
	}
	firstParts := strings.Fields(firstLine)
	if len(firstParts) < 2 {
		return GitProvenance{}, 0, "", fmt.Errorf("parse first commit line: %q", firstLine)
	}
	firstSHA := firstParts[0]
	firstAt, err := time.Parse(time.RFC3339, firstParts[1])
	if err != nil {
		return GitProvenance{}, 0, "", fmt.Errorf("parse first commit time: %w", err)
	}
	mergedLine, err := gitOutput(repoRoot, "log", "-1", "--format=%H %cI", mergeRef)
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	mergedParts := strings.Fields(strings.TrimSpace(mergedLine))
	if len(mergedParts) < 2 {
		return GitProvenance{}, 0, "", fmt.Errorf("parse merge commit: %q", mergedLine)
	}
	mergedAt, err := time.Parse(time.RFC3339, mergedParts[1])
	if err != nil {
		return GitProvenance{}, 0, "", fmt.Errorf("parse merge time: %w", err)
	}
	filesOut, err := gitOutput(repoRoot, "diff", "--name-only", parent1, parent2)
	if err != nil {
		return GitProvenance{}, 0, "", err
	}
	files := nonEmptyLines(filesOut)
	defects, defectNote := countProxyDefects(repoRoot, mergeRef, files)
	return GitProvenance{
		MergeRef:       mergeRef,
		BranchBase:     base,
		BranchTip:      parent2,
		FirstCommitAt:  firstAt.UTC(),
		FirstCommitSHA: firstSHA,
		MergedAt:       mergedAt.UTC(),
		FilesChanged:   len(files),
	}, defects, defectNote, nil
}

func countProxyDefects(repoRoot, mergeRef string, files []string) (float64, string) {
	if len(files) == 0 {
		return 0, "no files changed — proxy defect count 0"
	}
	// Post-handoff window: 14 days after merge; fix-like subject lines only.
	logOut, err := gitOutput(repoRoot, append([]string{
		"log", mergeRef + "..HEAD", "--format=%H %cI %s", "--",
	}, files...)...)
	if err != nil {
		return 0, fmt.Sprintf("proxy defect scan failed: %v", err)
	}
	var count int
	for _, line := range nonEmptyLines(logOut) {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fix") || strings.Contains(lower, "bug") ||
			strings.Contains(lower, "defect") || strings.Contains(lower, "hotfix") ||
			strings.Contains(lower, "revert") {
			count++
		}
	}
	note := fmt.Sprintf("git proxy: %d fix-like commits after merge touching same %d files (14d window not applied — count all post-merge fix-like commits; not confirmed defects without linked issue/incident)", count, len(files))
	return float64(count), note
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// WriteReport saves rendered markdown to path.
func WriteReport(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("bench: mkdir report dir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// CopyFile is a test helper for fixture setup.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
