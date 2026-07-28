# Backpressure runbook

## Symptom
Queue depth, concurrent workflows, or runner saturation rises faster than completions.

## Verify
- Inspect quota usage per profile.
- Check admission backlog and queue depth metrics.
- Confirm brownout shedding order matches policy.

## Mitigate
- Freeze improvement lanes first.
- Lower per-profile runner concurrency.
- Drain admissions until queue depth returns below threshold.

## Escalate
Escalate when customer-impacting delivery queues remain saturated for two scan windows.
