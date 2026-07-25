# Purpose

Testing discipline for a system whose entire value proposition is "evidence-based completion; no self-reported
done" (Constitution C10). A task is not done because code compiles — it's done because its Validation commands
produced evidence.

# Test pyramid for this repo

- **Unit** — table-driven, deterministic, no network/filesystem/Temporal dependency; the majority of tests.
- **Integration** — real Postgres/Temporal via the `dev` compose services (Task 4); gated by nothing (these are
  part of `make test` once those services exist), but must clean up their own state.
- **Gated live e2e** — real external providers (GitHub, Stripe, Fly, Claude Code) behind explicit flags
  (`RUN_GITHUB=1`, `RUN_STRIPE=1`, etc., per the `integration` agent's boundary) — never run unattended in CI
  against production credentials.
- **Deterministic validation runner** (Task 13) — the mechanism that turns "the executor said it's done" into
  evidence; this is infrastructure other tests build on, not a replacement for them.
- **Fault-injection / chaos** (Task 64) — forced kill, retry exhaustion, network partition; proves recovery
  semantics (C22), not just happy path.

# Required commands before calling any task done

```sh
go build ./...
go vet ./...
staticcheck ./...
go test -race -count=1 ./...
govulncheck ./...
```

Plus the task card's own Validation command(s), plus repo-wide `make test && make fitness`.

# What "covered" means here

- New behavior has a test that fails without the change and passes with it (verify this, don't assume it).
- Error paths and edge cases (empty input, max-size payload, concurrent access, context cancellation) are tested,
  not just the golden path.
- Flaky tests (timing-dependent `sleep`-based synchronization, unseeded randomness) are bugs, not "known
  flakiness" — fix the synchronization, don't retry-loop around it.
- Coverage on touched packages doesn't regress; a Rev R3/R4 task's coverage evidence is part of what
  `security-review` checks, not optional.

# Anti-Patterns

- Testing only the happy path because the task card's example only showed the happy path.
- Mocking out the exact boundary the test exists to verify (e.g. mocking the kernel's side-effect gate in a test
  that's supposed to prove the gate works).
- Treating a green CI run from a *different* branch/commit as evidence for this change.
- Skipping `-race` "to save time" on anything touching goroutines, channels, or shared state.
