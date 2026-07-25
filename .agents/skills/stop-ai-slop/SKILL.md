---
name: stop-ai-slop
description: "Name and block the specific failure modes of AI-generated code and AI-generated reports that pass a shallow check"
---

<!-- ars:source .ai/skills/stop-ai-slop/SKILL.md -->
# Purpose

Name and block the specific failure modes of AI-generated code and AI-generated reports that pass a shallow check
but cost the project real time later. This is a discipline skill, not a style skill — it governs what an agent is
allowed to claim, not how it formats code.

# The core rule

**Never report done what wasn't verified.** Constitution C10 ("Evidence-based completion; no self-reported done")
is not just a kernel rule about workflow status — it applies to every agent's own report at the end of a task. If
a validation command wasn't run, say so; don't imply it passed.

# Slop patterns to refuse

- **Placeholder implementations reported as complete** — `TODO`, `not implemented`, `panic("todo")`, or a stub
  that satisfies a type signature but not the task's Acceptance criteria, left in place without being called out
  as incomplete.
- **Fabricated or assumed test results** — describing a test suite as "passing" without having run it in this
  session, or extrapolating from a similar-looking prior run.
- **Silent scope inflation** — touching files or packages outside the task card's Scope "while I was in there,"
  then not mentioning it in the report.
- **Silent scope shrinkage** — skipping a Step or an Output because it was hard, without flagging it as a
  blocker per the no-gaps rule (`.ai/instructions/task-protocol.md`).
- **Comment noise** — comments that restate what the code already says (`// increment counter` above `i++`), or
  meta-comments about the task itself (`// fix for issue #123`, `// added per Task 14`) that rot as the codebase
  evolves. Comments earn their place only by explaining a non-obvious WHY.
- **Sycophantic or padded reporting** — "Great, everything looks good!", restating the request back before
  answering, disclaimers nobody asked for, or a summary longer than the change it describes.
- **Backwards-compatibility theater** — keeping a renamed variable's old name as an alias, leaving `// removed:
  ...` comments, or re-exporting a deleted type "just in case," when the task's Boundary says nothing about
  compatibility.
- **Confidently wrong file/line references** — citing a location without having just read it in this session.

# Process

1. Before reporting a task done, re-read the Acceptance list and match each item to a command you actually ran
   and a result you actually saw in this session's output — not a memory of a similar prior run.
2. If something couldn't be validated (missing tool, no network, no Docker daemon), say exactly that and name it
   as a blocker — do not round it up to "done."
3. Keep the report itself terse: changed files, what was validated and how, what's left, blockers. No preamble,
   no closing pleasantries, no restating the task back to the reader.

# Anti-Patterns

- Any of the slop patterns above.
- Treating this skill as a style guide (it isn't — `coding-standards` and `code-quality` own style/architecture;
  this skill owns honesty about what was actually done).
