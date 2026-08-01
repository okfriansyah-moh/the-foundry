# M5 Baseline — V1 Acceleration Control Arm (docs/PLAN.md Task 134 / ACC-01)

Date: 2026-08-01. This note documents how the control-arm baseline was derived
for bounded **V1 acceleration evidence** (Constitution C25, Blocker B12).

## Source

Per B12, the control arm is **mined from this repository's own git history** —
not hand-estimated. Three comparable prior deliveries were selected: merged PRs
each landing a multi-task wave of similar scope to the personal-path Foundry
proofs Task 135 will measure against.

| Run ID | Delivery | Merge commit | Files | First→merged (git proxy) |
| --- | --- | --- | ---: | ---: |
| `control-pr4-task40-50` | PR #4: Tasks 40–50 (venture foundation) | `a57783a` | 106 | ~3.0 h |
| `control-pr5-task51-60` | PR #5: Tasks 51–60 (mission/PEC evolution) | `e84fb31` | 51 | ~1.5 h |
| `control-pr6-task61-75` | PR #6: Tasks 61–75 (Track B exit + M2 hardening) | `c31ceb2` | 95 | ~0.7 h |

Git-derived timings use **first branch commit → merge** as a proxy for
`plan_to_first_accepted` and `plan_to_verified_completion` in the control arm
(flagged `proxy` — no PLAN artifact existed in the pre-Foundry workflow).

## Human-reported fields (B12)

Only two metrics cannot come from git and are entered explicitly in
`benchmarks/baseline/manifest.yaml`, flagged `human-reported`:

- `human_orchestration_time` (hours)
- `manual_prompts_touches` (count)

Values reflect the operator's log for each delivery window, not reconstructed
from commit timestamps.

## Proxy defects after handoff

`defects_after_handoff` counts post-merge commits touching the same files with
fix-like subjects (`fix`, `bug`, `defect`, `hotfix`, `revert`). These are
**proxy observations** unless linked issue/incident evidence confirms a defect;
the report renderer surfaces the proxy flag and does not relabel every later
edit as a confirmed defect.

## Artifacts

- `benchmarks/baseline/manifest.yaml` — delivery list + human input
- `benchmarks/baseline/*.json` — per-run `RunRecord` files
- `benchmarks/baseline/summary.json` — captured control-arm bundle
- `benchmarks/baseline/report.md` — comparison table (baseline only until Task 135)
- `config/benchmark-targets.yaml` — **V1 acceptance targets** (not universal claims)

## Regeneration

```bash
make bench-baseline
# equivalent:
docker compose -f deploy/docker-compose.yaml run --rm dev go run ./cmd/foundry bench baseline
```

## What this card does not claim

Task 134 **measures and records**; it makes no acceleration claim. Foundry-arm
comparison and threshold verdicts are Task 135 (ACC-02).
