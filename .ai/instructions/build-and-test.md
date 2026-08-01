# Build And Test

Verbatim from `docs/PLAN.md` §C (Conventions).

## Docker execution model

Every `make <target>` is a thin wrapper around `docker compose run --rm dev <real command>` (long-running services
use `up`/`down` instead). Target names and their meaning never change — only the implementation is containerized,
so every Validation command in every task card (`make test`, `go test ./...`, `bash test/foo.sh`, etc.) is run the
same way whether typed directly or through `make`; commands shown as bare `go test`/`go run`/`bash` are understood
to execute inside `dev` (either via a `make` target or `docker compose run --rm dev <cmd>` directly — an agent may
use either form). Host requirements: Docker Engine/Desktop + Docker Compose v2 + GNU make — no local Go, Node,
Playwright, or database install is ever required. CI builds and runs the identical `dev` image (dev/CI parity — no
"works on my machine"). Go module and build caches live in named Docker volumes so repeated runs stay fast.

## Make targets contract

Created Task 1, extended by later tasks; never renamed:

```
bootstrap up down doctor test lint fitness skp-e2e e2e-github e2e-venture e2e-tenx evidence-verify projection-rebuild
```

Each wraps `docker compose run --rm dev <cmd>` (or `up`/`down`); adding a target never changes an existing one's
name or docker-wrapping pattern.

## Container topology & network policy

Exactly four image lineages exist for the life of the plan, each with one owner task and one stated purpose. No
fifth image or second compose file may be added without a matching row in `docs/PLAN.md` §C — Task 37 lints for
it, so an ad hoc `Dockerfile.whatever` fails CI, not just code review.

| Image | Owner | Purpose | Network |
| --- | --- | --- | --- |
| `dev` | Task 1 | toolchain to build/test/run Foundry itself | full outbound internet |
| `postgres`, `temporal` | Task 4 | `dev`'s runtime dependencies | internal compose network only |
| `foundry-executor-sandbox` | Task 34 | isolates AI-agent-executed task code; ephemeral per-task container spawned by kernel Go code (not in compose — Task 115) | default-deny egress + narrow allowlist |
| product template's own image | Task 46 | the venture product's own runtime | governed by the product, not Foundry |
| `foundry` (release) | Task 73 | the shipped `foundry`/`foundryd` binaries | not applicable |

Two hard rules: (1) `deploy/docker-compose.yaml` holds only the long-running dev-time services (`dev`, `postgres`,
`temporal`) — one file, never a second. (2) The network default is open outbound everywhere; nobody hardens `dev`,
`postgres`, or `temporal` by restricting egress. The executor sandbox is the sole deliberate exception: default-deny,
because it's the one place potentially-arbitrary agent-generated code executes.
