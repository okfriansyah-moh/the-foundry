# Data Retention, Privacy, and PII

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Delivery Foundry stores customer data, prompts, mockups, and memory — all of it is regulated surface, including under Indonesian UU PDP 2022 where applicable.

## 1. Retention classes

Every stored object belongs to exactly one class:

| Class | Examples | Default posture |
|---|---|---|
| workflow-history | Temporal event history | TTL per profile; large payloads in object store |
| artifacts | evidence bundles, builds | TTL; content-addressed |
| prompts | task packets, agent transcripts | TTL; redaction rules apply |
| source-code | worktrees, patches | follows repository policy |
| visual-inputs | screenshots, mockups, Figma exports | TTL; may contain third-party PII |
| audit | admission, approval, promotion records | long retention; legal hold capable |
| customer-data | venture product user data | product privacy policy governs; strictest class |
| billing-data | payment metadata (never card data) | provider-of-record holds primary |
| memory | curated knowledge, embeddings | TTL + provenance; deletion cascades |
| vector-indexes | derived embeddings | deleted when source is deleted |
| logs | runtime logs | short TTL; redacted |
| notifications | Telegram/channel content | short TTL; classification-filtered |

## 2. Required controls

- TTL per class per profile; backup expiry aligned with TTL;
- legal hold that suspends deletion with an audit trail;
- encryption at rest and in transit for every class;
- deletion cascades to derived data: memory entries, vector indexes, caches, and projections derived from a deleted source MUST be deleted with it;
- access logging on customer-data, billing-data, audit, and memory classes;
- profile isolation: no cross-profile reads of memory, artifacts, or customer data;
- data-subject request handling (access, correction, deletion) for venture products, with UU PDP 2022 timelines where the product serves Indonesian users;
- customer data is never sent to external model providers unless the profile explicitly authorizes it per classification policy.


---

<!-- Relocated from V11: §17 Data classification policy (lines 10576-10622) -->

## 17. Data classification policy

Every profile declares a classification.

```text
public
personal
internal
confidential
restricted
```

Rules example:

```yaml
classification_rules:
  public:
    external_models: allowed
    public_research: allowed
    telegram: allowed

  personal:
    external_models: allowed
    public_research: allowed
    telegram: allowed

  internal:
    external_models: approved_only
    public_research: restricted
    telegram: forbidden

  confidential:
    external_models: forbidden_unless_contractually_approved
    public_research: forbidden
    telegram: forbidden
    logs_must_be_redacted: true

  restricted:
    autonomous_code_execution: human_approved_only
    external_models: forbidden
    export: forbidden
```

The policy engine must reject execution before any data leaves the boundary.

---

