# Operator Console Mockup

Non-normative design artifact. Explores IA/UX for a future Foundry operator web UI (§Q deferred). Not a product surface; not wired to the API.

**Visual system (ui-ux-pro-max):** Soft UI Evolution · light B2B slate · Lexend + Source Sans 3. Dense dashboard; skip-link; focus rings; `prefers-reduced-motion`; 44px controls; SVG/text status only (no emoji icons). Packaging + M8 config tables share the same tokens.

## Trust domain (must match codebase)

| Rule                                 | Mock behavior                                                           |
| ------------------------------------ | ----------------------------------------------------------------------- |
| One `FOUNDRY_PROFILE` per `foundryd` | Top-bar **This process** switcher                                       |
| Multi-profile = multi-process        | Switching reloads another deployment’s data (separate API host mock)    |
| `RefuseMultiProfileSingleProcess`    | No mixed venture+10x inbox/board in one session                         |
| Mission → workflows                  | Process `?mission=msn-01` filters by `mission_id`                       |
| `max_active_missions`                | Quota bar on Overview / Missions / Config → Quotas                      |
| Packaging enable lists               | Venture wider; Org 10x subset (no pec/backend; no commercial-readiness) |

Try: open Overview → switch to **Org 10x** → Packaging → Enablement / L1 evolve change. Switch back to **Venture**. Config → Layers / Quotas / Rates / Mission·Opp for DB propose flows.

## Open

```bash
open docs/mockups/operator-console/index.html
# or Packaging hub / Config layers:
open docs/mockups/operator-console/pages/packaging.html
open docs/mockups/operator-console/pages/config.html
```

## IA map

| Page        | Path                            | Job                                                      |
| ----------- | ------------------------------- | -------------------------------------------------------- |
| Overview    | `index.html`                    | Needs-you for this process                               |
| Process     | `pages/process.html?mission=`   | Workflow board scoped to deployment (+ optional mission) |
| Workflow    | `pages/workflow.html?id=`       | Timeline + phase/reason                                  |
| Approvals   | `pages/approvals.html`          | Gates for this process                                   |
| Approve     | `pages/approve.html?id=`        | High-risk WebAuthn (mock)                                |
| Missions    | `pages/missions.html`           | Missions in this process + Board link                    |
| Mission     | `pages/mission.html?id=`        | Loop snapshot; blocks cross-profile                      |
| Config      | `pages/config.html`             | YAML ceilings RO + editable DB LayerPolicy propose       |
| Quotas      | `pages/config-quotas.html`      | Quota table → propose (CFG-02)                           |
| Rates       | `pages/config-rates.html`       | Model rates → propose version (CFG-03)                   |
| Mission·Opp | `pages/config-mission-opp.html` | Decide knobs, L0 values, opp weights (CFG-02/04/05)      |
| Budgets     | `pages/budgets.html`            | Caps for this deployment                                 |
| Evidence    | `pages/evidence.html`           | Verify                                                   |
| Profiles    | `pages/profiles.html`           | This process vs other foundryd                           |
| Intake      | `pages/intake.html`             | Idea-intake (light)                                      |
| Packaging   | `pages/packaging.html`          | CAP-01–04 hub: validate + workspace summary              |
| Catalog     | `pages/packaging-catalog.html`  | Editable DB catalog → propose add/edit (CAP-04)          |
| Enablement  | `pages/packaging-enable.html`   | Per-process toggles → propose (CAP-04)                   |
| Install     | `pages/packaging-install.html`  | Materialize → `claude-code` + doctor (CAP-02)            |
| L1 evolve   | `pages/packaging-evolve.html`   | Promote / rollback; org proposal-only (CAP-03)           |

## YAML vs Postgres (Tasks 156–161)

| Stay YAML (rare / ceiling)                      | Postgres SoT (operator-hot)                         | Task   |
| ----------------------------------------------- | --------------------------------------------------- | ------ |
| `platform.yaml` + rego                          | Profile/org LayerPolicy overlays                    | CFG-01 |
| Sandbox / validation / signal allowlists        | Quotas + mission-decide knobs                       | CFG-02 |
| `executor-capabilities`, queue-priority         | Model rates + models (versioned)                    | CFG-03 |
| Tunables **bounds**; routing preference _lists_ | L0 tunable **values**                               | CFG-04 |
| Opportunity research domains / markers          | Opportunity scoring weights (+ numeric caps)        | CFG-05 |
| Deploy / CI / OpenAPI / `.ai` / fixtures        | Agent/skill **catalogs** + packaging **enablement** | CAP-04 |

Authority path for every DB row: **table edit → Propose → Approvals → kernel apply**. No ungated browser write.

**Risk (catalogs):** DB-authored packages are higher supply-chain risk than FS+PR — CAP-04 requires content pins, fail-closed validate (reviewer≠implementer), and no `executor_allowlist` widen on install.

## Packaging model (Tasks 153–155 + 161)

| Layer            | Mock shows                                                                  |
| ---------------- | --------------------------------------------------------------------------- |
| Foundry catalogs | Postgres SoT (seeded from `agents/` + `skills/` catalogs); editable propose |
| Enablement       | Per-process toggles; enabled ⊆ catalog                                      |
| Install          | `claude-code` materializer; validate gate; doctor; allowlist unchanged      |
| L1 → disk        | Personal active versions + rollback; org proposals only                     |
| Not shown as hub | OpenHands / 9Router skill management (ADR-001 externals)                    |

Nav injects **Product packages → Packaging** on all pages via `assets/app.js`. Config subnav (Layers / Quotas / Rates / Mission·Opp) mounts on config pages.

## Authority framing

- Kernel owns side effects (C4). UI only proposes.
- PEC proposes waves; never decides (C5).
- High-risk approve = WebAuthn; Telegram never approves high-risk (C11).
- Config + packaging catalog/enablement = propose → Approvals → admission/apply DB.
- Status registry: `PENDING` · `RUNNING` · `WAITING` · `SUCCEEDED` · `FAILED` · `CANCELLED`.

## Non-goals

No `internal/*` changes, no OpenAPI expansion, no Go for M8 in this mockup pass, no real auth.
