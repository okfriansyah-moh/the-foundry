# ADR-000 — Why build Delivery Foundry at all?

[← Back to Delivery Foundry master index](../../../delivery_foundry.md) · [Migration map](../../../docs/MIGRATION_MAP_V11_TO_V12.md)

Status: Accepted. Uniqueness is claimed only where the comparison shows it; everything reusable is reused.

| System | Reused | Wrapped | Missing (why Foundry remains necessary) | Rebuild risk | Exit strategy |
|---|---|---|---|---|---|
| Temporal | Durable execution, timers, retries, history | Workflow backend behind kernel interfaces | No plan admission, policy, evidence, agent authority, autonomy envelopes | High if reimplemented — never rebuild durable execution | Kernel workflow interface allows another durable engine |
| Argo Workflows | — | Possible runner substrate on K8s | DAGs are static; no admitted-plan semantics, no agent loops, no policy compilation | Medium | Runner contract is pluggable |
| GitHub Actions / Bitbucket Pipelines | CI checks as evidence sources | CI contract adapter | No durable multi-day loops, no cross-repo waves, no autonomy governance | Low — used as-is | Already adapter-based |
| OpenHands and coding-agent platforms | Agent execution patterns | Executor adapters (execution classes) | No mission economics, admission tiers, drift governance, org provenance | Medium | Executor adapter contract |
| Backstage | — | Possible catalog UI later | Developer portal, not an execution control plane | Low | None needed |
| Agent-skill frameworks (Superpowers etc.) | Skills and methodology packs | Extension registry with pinning and evaluation | No authority model, no promotion governance | Low | Registry maps or disables conflicts |
| OPA | Runtime authorization PDP | PDP behind policy interface | Does not merge layered config or enforce non-weakening precedence — that is the configuration compiler's job | Low | PDP interface is standard |
| Notification systems | Telegram Bot API | Notification contract | Batching/veto-digest governance semantics are Foundry-specific | Low | Contract-based |

**Decision:** Delivery Foundry is the thin-but-authoritative layer none of these provide together: deterministic plan admission with tiered autonomy, evidence-based completion, kernel-owned authority over agent proposals, mission economics, and drift governance — composed over Temporal, OPA, existing CI, and existing executors rather than replacing them. If a future platform ships admitted-plan autonomy governance natively, the adapter seams above are the exit.
