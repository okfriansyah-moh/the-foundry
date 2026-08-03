# Capability packaging and runtime authority

Foundry's `agents/catalog.yaml`, `skills/catalog.yaml`, canonical agent files,
and canonical skill packages are the source of truth. A product's
`.foundry/skills/enabled.yaml` selects a validated subset of those catalogs.
The selected runtime provider does not own or redefine either declaration.

For Claude Code, `foundry agents install` and `foundry skills install` copy the
enabled canonical content into workspace-local `.claude/agents/` and
`.claude/skills/` projections. The files under `.claude/` are derived copies,
not the Foundry catalog. Per-kind manifests under
`.foundry/agent-runtime/claude-code/` pin the catalog, enablement, paths, and
content digests so reinstall is deterministic and `doctor` detects drift or a
missing managed file.

The workspace manifest is not write or deletion authority. Installation only
creates absent projection files or accepts files whose bytes already match the
expected projection. A changed catalog or enablement pin therefore fails closed
in an already-materialized workspace; materialize the new inputs into a fresh
workspace instead of replacing workspace-controlled files.

Installing an agent or skill does not add an executor, change any
`executor_allowlist`, select an executor, or grant execution authority. It
also grants no SCM-write or deploy authority. Kernel and policy remain the
decision and side-effect boundary; materialization only writes the selected
provider's package files beneath the product workspace.

Claude Code is the only materializer supplied by Task 154. OpenHands and
9Router remain deferred provider adapters. They are not required to validate,
install, or diagnose the default package projection, and neither becomes the
management plane for Foundry capabilities.

## Bounded skill evolution

The L1 evolution bridge connects the bounded `SkillRegistry` pipeline to these
same canonical package inputs. A successful personal-profile promotion appends
an immutable `skills/<name>/versions/vN/SKILL.md`, advances only that skill's
personal-venture catalog source (the organization/default source remains on
the baseline), and records the result in the append-only promotion log. A
rollback is another append: it restores the preceding materialization bytes as
a new version while retaining every earlier version for audit.

```sh
foundry skills rollback -root <foundry-root> -skill <catalog-skill-name>
```

The rollback command reconstructs the current and previous immutable versions
from the catalog and version tree, so it remains usable after a process restart.
It reads `PG_DSN` (or `--pg-dsn`) lazily: ordinary rollbacks remain hermetic,
while a rollback that itself crosses the cumulative budget requires the
PostgreSQL freeze store and fails closed if that durable brake is unavailable.
For the daemon-wide skill-evolution freeze, clear the durable `global` scope
with `foundry promotions unfreeze --product global` (or pass
`--freeze-scope global` explicitly).

Organization-profile candidates remain proposal-only and do not change the
catalog, package versions, product enablement, or installed runtime projection.
Permission or data-class expansion, budget increases, evaluation failures, and
the cumulative-drift freeze all stop before package activation. These rules are
the disk-backed continuation of the
[capability evolution workflow](../foundry/docs/workflows/capability-evolution.md)
and [cumulative drift governance](../foundry/docs/autonomy/cumulative-drift-governance.md);
they do not add authority to the runtime materializer.
