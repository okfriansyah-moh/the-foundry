# Control-Plane Self-Protection

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** The capacity broker protects model providers; this document protects the Foundry itself.

Required mechanisms:

- API rate limits on every ingress surface (API, webhooks, Telegram commands);
- bounded queue depth with explicit rejection over silent unbounded growth;
- tenant/profile fairness: one profile cannot starve others;
- per-profile concurrency limits for workflows, runners, and admissions;
- worker saturation monitoring with load shedding;
- runner pool quotas per profile and per workflow class;
- database connection pool protection (per-service ceilings, statement timeouts);
- notification backpressure (batching engine degrades to digests, never drops P0);
- admission backpressure: new plan intake pauses before execution capacity collapses;
- priority lanes: security and recovery work preempts routine work;
- circuit breakers on every external dependency;
- brownout modes: shed lowest-priority loops first (learning, memory curation) and keep delivery and recovery alive;
- dead-letter handling with alerting for poisoned work items.

Webhook ingestion (preserved below) is one protected ingress among these.


---

<!-- Relocated from V11: §13 Webhook ingestion (lines 7973-8009) -->

## 13. Webhook ingestion

All providers send events into one normalized event receiver.

```json
{
  "event_id": "provider-delivery-id",
  "provider": "bitbucket-cloud",
  "event_type": "change_request.updated",
  "occurred_at": "2026-07-18T12:00:00Z",
  "workspace": "acme-engineering",
  "repository": "platform-api-service",
  "actor": {
    "id": "provider-user-id"
  },
  "resource": {
    "id": "123",
    "url": "provider-resource-url"
  },
  "raw_reference": "encrypted-or-external-payload-reference"
}
```

Webhook receiver rules:

1. Verify the provider signature or shared secret.
2. Reject stale timestamps where supported.
3. Use the provider delivery ID as the idempotency key.
4. Persist the event before processing.
5. Return success quickly.
6. Process asynchronously.
7. Record every resulting action.
8. Support replay without duplicating actions.

---


