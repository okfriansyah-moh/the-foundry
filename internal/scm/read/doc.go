// Package read is the read-only half of the Task 27 (FND-08) split of
// internal/scm: Mirror, Fetch, and ResolveRef carry no authority
// restriction and are importable from anywhere in this repo (Constitution
// C4 restricts SCM *writes*, not reads, to the kernel — see
// internal/scm/write's doc.go for the half that is restricted).
//
// Every operation here uses github.com/go-git/go-git/v5 rather than
// shelling out to the git binary, so there is no argv/shell-injection
// surface from repoURL/mirrorPath/ref values reaching an exec.Command call
// (mirroring the argv-safety discipline internal/worktree established for
// its own, narrower exec.Command usage).
package read
