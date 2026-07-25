package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// repoLock is an exclusive, per-repo advisory lock backed by a file in
// root/.locks. It serializes Acquire/Release against the same repoPath so
// concurrent `git worktree add`/`remove` calls on that repo never race
// (docs/PLAN.md Task 9 Step 2).
type repoLock struct {
	f *os.File
}

// lockRepo opens (creating if needed) a lock file for repoKey under
// root/.locks and blocks until an exclusive flock is held. Callers must
// call unlock() to release it.
func lockRepo(root, repoKey string) (*repoLock, error) {
	dir := filepath.Join(root, ".locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("worktree: create lock dir: %w", err)
	}

	path := filepath.Join(dir, repoKey+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("worktree: open lock file %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("worktree: flock %s: %w", path, err)
	}

	return &repoLock{f: f}, nil
}

// unlock releases the flock and closes the underlying file descriptor.
func (l *repoLock) unlock() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		l.f.Close()
		return fmt.Errorf("worktree: unlock: %w", err)
	}
	return l.f.Close()
}
