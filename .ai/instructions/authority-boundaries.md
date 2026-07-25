# Authority Boundaries

Constitution articles C4 and C5, verbatim from `docs/PLAN.md` §B — this is the enforcement contract every agent's
`Boundaries` section must not contradict.

| #  | Article | Enforced by |
| --- | --- | --- |
| C4 | Kernel owns sequencing, retries, leases, fencing, state, policy, budgets, **all side effects incl. SCM writes** | Task 12, 27, 28 |
| C5 | PEC only proposes waves/dispatch/remediation; prohibition-tested | Task 56 |

## What this means for dispatch

- Only `internal/kernel` performs side effects.
- Only the `go-kernel` agent is ever dispatched against `internal/kernel` or `internal/scm/write`.
- `internal/pec` proposes waves, dispatch, and remediation — it never decides, and it is prohibition-tested against
  ever gaining decision authority.
- No other agent role (`go-backend`, `integration`, `infra`, `web`, `security-review`) may be dispatched against
  `internal/kernel`, `internal/scm/write`, or `internal/pec`'s decision path.
