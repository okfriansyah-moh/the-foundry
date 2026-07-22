// Package worktree implements per-task isolated git worktrees (Constitution
// C8): every bounded task gets its own working directory and branch, and no
// operation in this package ever runs a mutating git command against a
// canonical repository path itself — only against worktree paths created
// under Manager.Root. Local repos only; no pushes, no remotes.
//
// Exec role: go-backend (docs/PLAN.md Task 9 / SKP-07).
package worktree
