# PR Remediation

## Use

Review a pull request diff for this repo and produce concise, actionable remediation guidance.

## Inputs

- PR title, description, and changed files
- Key diff snippets or a summary of affected code
- Relevant invariants from `docs/architecture.md` and `docs/PLAN.md` (constitution articles, authority boundaries)
- Failing tests or review comments, if available

## Instructions

- Format every finding as: `[<severity>] <file:line> — <finding> → <exact fix>`. No prose filler, no hedging.
- Order findings by severity, most severe first.
- Validate against this repo's invariants: constitution articles C1–C22, the dispatched agent's `Boundaries` in
  `.ai/agents/<role>/AGENT.md`, and the `.ai/` canonical-source rule (composed provider artifacts are never
  hand-edited).
- For security-relevant findings, cite the OWASP Top 10 category (`.ai/skills/security-hardening/SKILL.md`) or
  OWASP LLM Top 10 category (`.ai/skills/ai-vulnerability-defense/SKILL.md`), not just "this looks insecure."
- Cite the exact rule or file the finding violates.
- If no issues are found, say so in one line and list any residual risk or command not run.

## Check

- Output is concise and actionable.
- Every finding cites a repo invariant or file.
- No generic or polite wording.
- Fix guidance is specific and immediately applicable.
