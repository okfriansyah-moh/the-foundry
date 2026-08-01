# V1 acceleration benchmark report

**Threshold config:** V1 acceptance targets (bench-targets-v1)

Control runs: 3 · Foundry runs: 0 · Overall: **baseline only**

## Per-metric comparison

| Metric | Control | Control basis | Foundry | Foundry basis | Verdict | Notes |
| --- | ---: | --- | ---: | --- | --- | --- |
| AI / provider cost | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Cost per accepted task | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Defects after handoff | 1.00 | proxy (proxy) | — | not-measurable | baseline only | foundry arm not recorded yet (Task 135) |
| Evidence rejection rate | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Human orchestration time | 25.33 | human-reported | — | not-measurable | baseline only | foundry arm not recorded yet (Task 135) |
| Integration conflicts | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Manual prompts / touches | 69.33 | human-reported | — | not-measurable | baseline only | foundry arm not recorded yet (Task 135) |
| PLAN → first accepted change | 2.01 | proxy (proxy) | — | not-measurable | baseline only | foundry arm not recorded yet (Task 135) |
| PLAN → 10x branch handoff | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| PLAN → verified completion | 2.01 | proxy (proxy) | — | not-measurable | baseline only | foundry arm not recorded yet (Task 135) |
| Recovery time | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Requirement → executable PLAN | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Retry rate | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Unattended runtime | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |
| Unauthorized actions | — | not-measurable | — | not-measurable | insufficient data | no measurable control observations |

## Quality guard

Status: **baseline recorded**

- foundry arm absent — quality guard baseline recorded, gate pending Task 135

## V1 acceptance targets (not universal claims)

### Personal path

- Manual orchestration reduction: ≥50%
- Delivery lead time reduction: ≥30%
- Unauthorized actions: ≤0

### 10x path

- PLAN → handoff reduction: ≥25%
- Coordination/reporting reduction: ≥30%
- Unauthorized SCM operations: ≤0

_Quality no worse than baseline: defects after handoff, evidence rejection rate, and rework must not regress beyond configured tolerances._
