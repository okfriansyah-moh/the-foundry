// Package fake implements a deterministic executor.Adapter driven entirely
// by a testdata script (fake_script.yaml). It exists so every test that
// needs an executor — including tests that must prove the kernel does not
// trust an executor's self-report (Task 13) — can drive one without
// spawning real subprocesses or calling a real LLM provider.
//
// The fake performs no network calls and is never a stand-in for a real
// provider adapter (Task 17 owns those); it only ever reads its script and
// writes inside the worktree.Workspace it is given.
package fake
