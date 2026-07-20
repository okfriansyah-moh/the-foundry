# Purpose

Apply application-security baseline (OWASP Top 10) to every task that touches `internal/*`, migrations, adapters,
or infra — before a `security-review` pass, not instead of one.

# Inputs

- `docs/foundry/docs/security/authorization-model.md` — TCB, prompt-injection defense, sandboxing (this repo's
  own authority model; it supersedes generic guidance where the two differ).
- `docs/foundry/docs/security/supply-chain.md`, `docs/foundry/docs/security/data-retention-and-privacy.md`.
- OWASP Top 10:2025 (`https://owasp.org/Top10/2025/`) — current as of this skill's authoring; re-check the source
  if it's been more than a year.

# OWASP Top 10:2025 mapped to this codebase

| # | Category | Where it applies here |
| --- | --- | --- |
| A01 | Broken Access Control (SSRF folded in) | `internal/admission`, OPA PDP integration (Task 23); every external HTTP call an adapter makes must not accept an attacker-controlled URL/host unchecked. |
| A02 | Security Misconfiguration | `deploy/*`, container topology (`.ai/instructions/build-and-test.md`) — no new image/egress path without a matching row in that table. |
| A03 | Software Supply Chain Failures | `go.mod`/`go.sum` pinning, `deploy/Dockerfile.dev` tool installs (`go install ...@latest` is acceptable only for dev tooling, never for anything that ships — pin versions for release builds, Task 73). |
| A04 | Cryptographic Failures | Ed25519 plan-approval signatures (Task 8), OIDC+WebAuthn (Task 25) — never roll a custom scheme where a stdlib/well-audited one exists. |
| A05 | Injection | See "Injection, concretely" below. |
| A06 | Insecure Design | Kernel-owns-side-effects (C4), PEC-proposes-only (C5) are themselves insecure-design mitigations — don't design around them for convenience. |
| A07 | Authentication Failures | OIDC+WebAuthn for high-risk approvals (C12) — Telegram is explicitly never a substitute (C11). |
| A08 | Software/Data Integrity Failures | ApprovedPlan provenance chain (C7) — authorship is never authorization; verify signatures, don't trust headers/claims. |
| A09 | Security Logging and Alerting Failures | Observability baseline (Task 31) — security-relevant events (auth failures, admission rejections, sandbox escapes) must be logged with enough context to investigate, without logging secrets. |
| A10 | Mishandling of Exceptional Conditions | Recovery/checkpoint/restart (C22) — a panic or unhandled error must resolve to an honest `PROVEN_BLOCKED`, never a silent retry loop or a swallowed error. |

# Injection, concretely (A05)

- **SQL:** always parameterized queries/prepared statements (`$1`/`?` placeholders depending on driver) — never
  string-concatenate or `fmt.Sprintf` a query with external input. Least-privilege DB role for the app connection.
  Run `gosec` (already relevant once CI/lint expands) to catch unparameterized queries.
- **Command injection:** never build a shell command string from external input; use `exec.Command(name, args...)`
  with args as a slice, never through `sh -c "<concatenated string>"`.
- **Path traversal:** any file path built from external input (executor workspaces, evidence bundles, mockup
  ingestion) must be cleaned and verified to stay inside its intended root — reject `..` escapes explicitly, don't
  rely on string matching alone.
- **SSRF:** any outbound HTTP call whose target is influenced by external input (webhook URLs, mockup fetch
  URLs) must validate against an allowlist, not a denylist, per the executor-sandbox default-deny model
  (`docs/PLAN.md` §C container topology).

# Process

- Treat every external input (PLAN submission, mockup, webhook payload, Telegram command, executor output) as
  untrusted until validated against an explicit schema/allowlist.
- Secrets: never in code, logs, error messages, or evidence bundles. Use the secrets interface (Task 35) once it
  exists; until then, environment variables injected at runtime, never committed defaults.
- For anything touching auth/crypto/side effects, name the OWASP category and the constitution article it maps
  to in your self-review — that pairing is what `security-review` will check first.

# Anti-Patterns

- "We'll harden it later" for anything in the A01–A05 set above.
- Denylist-based input validation where an allowlist is feasible.
- Logging full request/response bodies without redacting secrets/PII (see
  `docs/foundry/docs/security/data-retention-and-privacy.md`).
- Widening `dev`/`postgres`/`temporal` egress to "fix" a build issue instead of finding the actual missing
  allowlist entry on the executor sandbox.
