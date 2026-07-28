// Package cliexec is the shared, provider-agnostic implementation of a
// headless CLI executor adapter. It factors out the plumbing every
// subprocess-based executor needs — writing the task prompt to a
// fixed-name file inside the workspace jail (so no packet field can
// influence the path), scrubbing the environment down to a fixed
// package-confined allowlist, feeding the prompt on stdin (never argv, so
// no argv/shell injection), and killing the whole process group on
// timeout/cancel — exactly matching Task 17's proven claude-code shape.
//
// docs/PLAN.md Tasks 86–89 (PRV-03..06). Each concrete provider package
// (opencode, geminicli, cursor, copilot, windsurf) supplies only its own
// Config: binary name, headless args, env allowlist, and result parser.
// All provider-specific detail stays confined to that package; this package
// contains no provider knowledge and makes no routing/selection decision
// (that is the kernel's job, Task 85).
//
// Adapters built here are stateless per invocation: New returns a fresh
// *Adapter holding no shared mutable state, satisfying the fresh-context
// contract Task 91 tests.
package cliexec
