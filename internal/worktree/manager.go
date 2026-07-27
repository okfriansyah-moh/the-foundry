package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// identPattern restricts workflow and task identifiers to a safe charset.
// It rejects "/", "..", and any character that could escape Root when
// joined into a filesystem path.
var identPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Workspace is an isolated git worktree leased to exactly one task.
type Workspace struct {
	// Path is the absolute filesystem path of the worktree.
	Path string
	// Branch is the branch checked out in the worktree, named
	// foundry/<wfID>/<taskID>.
	Branch string
	// Release removes the worktree and deletes its branch. It is safe to
	// call at most once; callers must call it when the task reaches a
	// terminal state.
	Release func() error
}

// Manager creates and reclaims isolated worktrees under Root. Root holds
// one directory tree per workflow/task pair plus a hidden .locks and .meta
// directory used internally; it is never itself a git repository.
type Manager struct {
	Root string
}

// meta is the on-disk record SweepOlderThan uses to find and reclaim
// orphaned worktrees without depending on in-memory Manager state.
type meta struct {
	RepoPath  string    `json:"repo_path"`
	Branch    string    `json:"branch"`
	CreatedAt time.Time `json:"created_at"`
}

// Acquire creates a new isolated worktree for (wfID, taskID) off repoPath's
// current branch, on a new branch named foundry/<wfID>/<taskID>. All
// mutating git operations run with -C pointed at repoPath (to read its
// config and refs) but only ever write into the new worktree directory —
// repoPath's own working tree and index are never touched.
func (m *Manager) Acquire(ctx context.Context, repoPath, wfID, taskID string) (Workspace, error) {
	if err := validateIdent(wfID); err != nil {
		return Workspace{}, fmt.Errorf("worktree: workflow id: %w", err)
	}
	if err := validateIdent(taskID); err != nil {
		return Workspace{}, fmt.Errorf("worktree: task id: %w", err)
	}

	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return Workspace{}, fmt.Errorf("worktree: resolve repo path: %w", err)
	}
	if info, err := os.Stat(absRepo); err != nil || !info.IsDir() {
		return Workspace{}, fmt.Errorf("worktree: repo path %s: not a directory", absRepo)
	}

	wsPath := filepath.Join(m.Root, wfID, taskID)
	branch := fmt.Sprintf("foundry/%s/%s", wfID, taskID)

	lock, err := lockRepo(m.Root, repoKey(absRepo))
	if err != nil {
		return Workspace{}, err
	}
	defer func() { _ = lock.unlock() }()

	if _, err := os.Stat(wsPath); err == nil {
		return Workspace{}, fmt.Errorf("worktree: %s already exists", wsPath)
	}

	base, err := currentBranch(ctx, absRepo)
	if err != nil {
		return Workspace{}, err
	}

	if err := os.MkdirAll(filepath.Dir(wsPath), 0o755); err != nil {
		return Workspace{}, fmt.Errorf("worktree: create parent dir: %w", err)
	}

	// #nosec G204 -- repoPath/branch/wsPath/base are argv elements passed
	// to exec.Command directly, never interpolated into a shell string;
	// wfID/taskID are validated against identPattern above.
	cmd := exec.CommandContext(ctx, "git", "-C", absRepo, "worktree", "add", "-b", branch, wsPath, base)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Workspace{}, fmt.Errorf("worktree: git worktree add: %w: %s", err, out)
	}

	if err := writeMeta(m.Root, wfID, taskID, meta{RepoPath: absRepo, Branch: branch, CreatedAt: time.Now().UTC()}); err != nil {
		return Workspace{}, err
	}

	ws := Workspace{Path: wsPath, Branch: branch}
	ws.Release = func() error {
		return m.release(absRepo, wfID, taskID, branch)
	}
	return ws, nil
}

// release removes the worktree at root/wfID/taskID and deletes its branch,
// serialized against the same repo via the per-repo lock. It is called by
// Workspace.Release and by SweepOlderThan.
func (m *Manager) release(repoPath, wfID, taskID, branch string) error {
	lock, err := lockRepo(m.Root, repoKey(repoPath))
	if err != nil {
		return err
	}
	defer func() { _ = lock.unlock() }()

	wsPath := filepath.Join(m.Root, wfID, taskID)

	// #nosec G204 -- see Acquire; argv only, no shell interpolation.
	rm := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wsPath)
	if out, err := rm.CombinedOutput(); err != nil && !strings.Contains(string(out), "is not a working tree") {
		return fmt.Errorf("worktree: git worktree remove: %w: %s", err, out)
	}

	del := exec.Command("git", "-C", repoPath, "branch", "-D", branch)
	if out, err := del.CombinedOutput(); err != nil && !strings.Contains(string(out), "not found") {
		return fmt.Errorf("worktree: git branch -D: %w: %s", err, out)
	}

	removeMeta(m.Root, wfID, taskID)
	return nil
}

// SweepOlderThan removes every worktree recorded under Root whose Acquire
// call happened more than d ago, regardless of which Manager instance (or
// process) created it — the state lives in root/.meta, not in memory.
func (m *Manager) SweepOlderThan(d time.Duration) error {
	metaDir := filepath.Join(m.Root, ".meta")
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("worktree: read meta dir: %w", err)
	}

	cutoff := time.Now().Add(-d)
	var errs []error
	for _, wfEntry := range entries {
		if !wfEntry.IsDir() {
			continue
		}
		wfID := wfEntry.Name()
		taskFiles, err := os.ReadDir(filepath.Join(metaDir, wfID))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, tf := range taskFiles {
			taskID := strings.TrimSuffix(tf.Name(), ".json")
			mt, err := readMeta(m.Root, wfID, taskID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if mt.CreatedAt.After(cutoff) {
				continue
			}
			if err := m.release(mt.RepoPath, wfID, taskID, mt.Branch); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("worktree: sweep: %v", errs)
	}
	return nil
}

func validateIdent(id string) error {
	if id == "" {
		return fmt.Errorf("empty identifier")
	}
	if !identPattern.MatchString(id) {
		return fmt.Errorf("invalid identifier %q: must match %s", id, identPattern.String())
	}
	return nil
}

func repoKey(absRepoPath string) string {
	sum := sha256.Sum256([]byte(absRepoPath))
	return hex.EncodeToString(sum[:])
}

func currentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("worktree: resolve current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func metaPath(root, wfID, taskID string) string {
	return filepath.Join(root, ".meta", wfID, taskID+".json")
}

func writeMeta(root, wfID, taskID string, mt meta) error {
	path := metaPath(root, wfID, taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("worktree: create meta dir: %w", err)
	}
	b, err := json.Marshal(mt)
	if err != nil {
		return fmt.Errorf("worktree: marshal meta: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("worktree: write meta: %w", err)
	}
	return nil
}

func readMeta(root, wfID, taskID string) (meta, error) {
	b, err := os.ReadFile(metaPath(root, wfID, taskID))
	if err != nil {
		return meta{}, fmt.Errorf("worktree: read meta: %w", err)
	}
	var mt meta
	if err := json.Unmarshal(b, &mt); err != nil {
		return meta{}, fmt.Errorf("worktree: unmarshal meta: %w", err)
	}
	return mt, nil
}

func removeMeta(root, wfID, taskID string) {
	_ = os.Remove(metaPath(root, wfID, taskID))
}
