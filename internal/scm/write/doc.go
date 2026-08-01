// Package write is the write half of the Task 27 (FND-08) split of
// internal/scm. It performs exactly one side effect — pushing a branch —
// and must be importable ONLY from internal/kernel (Constitution C4:
// kernel owns all side effects, including SCM writes). The actual
// compile-time/CI enforcement of that import restriction is Task 28's job
// (cmd/fitlint's go-list-deps graph assertion); this package is written as
// if that boundary already holds — in this repo today, internal/kernel is
// in fact the only importer (see internal/kernel/scmpush.go).
//
// Boundary (non-negotiable, Task 27's own card): no PR-creation API of any
// shape exists anywhere in this package — PushBranch performs a branch
// push and nothing else. No force-push code path exists anywhere, not
// behind a flag, not as dead/unused code: every git.PushOptions this
// package constructs leaves Force at its zero value (false), and CAS is
// enforced instead via git.PushOptions.RequireRemoteRefs plus a plain
// (non-force) update refspec — a real round trip to the remote's own
// git-receive-pack (or, for the file:// transport this package's own
// fixture tests use, the real local git-receive-pack binary go-git shells
// out to) that atomically rejects the push if the remote ref has moved,
// not a client-side comparison against a value read earlier.
//
// Secrets: authentication uses TokenSource/EnvTokenSource in secrets.go
// (environment variable) and SecretsTokenSource (internal/secrets.Store via
// Task 35 / Task 137) behind interfaces so production wiring can select the
// right source per profile without changing Pusher.
package write
