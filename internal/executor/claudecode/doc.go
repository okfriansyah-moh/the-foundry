// Package claudecode implements executor.Adapter by shelling out to the
// `claude` CLI (Claude Code) in non-interactive print mode. It is
// registered under the name "claude-code" and selected via
// FOUNDRY_EXECUTOR=claude-code (docs/PLAN.md Task 17 / SKP-15).
//
// Verified CLI flags and their source are recorded, dated, in
// docs/notes/claude-code-flags.md — re-verify against the installed
// binary before relying on this adapter unattended (staleness rule,
// docs/foundry/docs/providers/anthropic.md).
//
// Auth credential: by default Adapter relies on ANTHROPIC_API_KEY/
// CLAUDE_CODE_OAUTH_TOKEN already being present in the process
// environment (passed through to the subprocess via allowedEnv, nothing
// else). Setting Adapter.Secrets (Task 35 / FND-16's secrets seam)
// additionally fetches the configured credential from that store and
// injects it into the process environment for the duration of Run,
// restoring whatever was there before — this is the "executor auth"
// existing env usage Task 35's card names for migration.
//
// PhaseHint (docs/PLAN.md Task 92 / PRV-09): when a TaskPacket carries a
// non-empty PhaseHint, renderPrompt includes it as a labeled, informational
// section in the prompt file — never in argv, never in a shell string. The
// hint is strictly ONE-DIRECTIONAL (kernel → executor) and carries NO
// authority: an executor cannot use it to request elevated permissions, skip
// validation, alter its EnvAllowlist or allowlisted commands, or signal
// completion. Honest completion remains solely the job of Task 13's
// verify.Runner (Constitution C10) — a lying Summary is judged identically
// with or without any PhaseHint value. An empty PhaseHint produces a
// byte-identical prompt to the pre-Task-92 behavior.
package claudecode
