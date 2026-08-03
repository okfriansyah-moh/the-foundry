# Operator Console Mockup

Non-normative design artifact. Explores IA/UX for a future Foundry operator web UI (§Q deferred). Not a product surface; not wired to the API.

**Visual system (ui-ux-pro-max):** Soft UI Evolution · light B2B slate · Lexend + Source Sans 3. Dense dashboard; skip-link; focus rings; `prefers-reduced-motion`.

## Trust domain (must match codebase)

| Rule                                 | Mock behavior                                                        |
| ------------------------------------ | -------------------------------------------------------------------- |
| One `FOUNDRY_PROFILE` per `foundryd` | Top-bar **This process** switcher                                    |
| Multi-profile = multi-process        | Switching reloads another deployment’s data (separate API host mock) |
| `RefuseMultiProfileSingleProcess`    | No mixed venture+10x inbox/board in one session                      |
| Mission → workflows                  | Process `?mission=msn-01` filters by `mission_id`                    |
| `max_active_missions`                | Quota bar on Overview / Missions                                     |

Try: open Overview → switch to **Org 10x** → missions/process/approvals change. Switch back to **Venture**.

## Open

```bash
open docs/mockups/operator-console/index.html
```

## IA map

| Page      | Path                          | Job                                                      |
| --------- | ----------------------------- | -------------------------------------------------------- |
| Overview  | `index.html`                  | Needs-you for this process                               |
| Process   | `pages/process.html?mission=` | Workflow board scoped to deployment (+ optional mission) |
| Workflow  | `pages/workflow.html?id=`     | Timeline + phase/reason                                  |
| Approvals | `pages/approvals.html`        | Gates for this process                                   |
| Approve   | `pages/approve.html?id=`      | High-risk WebAuthn (mock)                                |
| Missions  | `pages/missions.html`         | Missions in this process + Board link                    |
| Mission   | `pages/mission.html?id=`      | Loop snapshot; blocks cross-profile                      |
| Config    | `pages/config.html`           | Layered browse + propose                                 |
| Budgets   | `pages/budgets.html`          | Caps for this deployment                                 |
| Evidence  | `pages/evidence.html`         | Verify                                                   |
| Profiles  | `pages/profiles.html`         | This process vs other foundryd                           |
| Intake    | `pages/intake.html`           | Idea-intake (light)                                      |

## Authority framing

- Kernel owns side effects (C4). UI only proposes.
- PEC proposes waves; never decides (C5).
- High-risk approve = WebAuthn; Telegram never approves high-risk (C11).
- Config = propose → Approvals → admission.
- Status registry: `PENDING` · `RUNNING` · `WAITING` · `SUCCEEDED` · `FAILED` · `CANCELLED`.

## Non-goals

No `internal/*` changes, no OpenAPI expansion, no PLAN task, no real auth.
