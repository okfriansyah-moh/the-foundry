# docs/PLAN.md Task 34 (FND-15): the `foundry-executor-sandbox` image
# lineage (CLAUDE.md container topology table — owner: this task; purpose:
# isolates AI-agent-executed task code; network: default-deny egress +
# narrow allowlist). This is the ONE image both roles in
# internal/executor/sandbox run: the task sandbox itself (go/node/git/task
# tooling, entrypoint = whatever command the executor asks for) and the
# egress-gate sidecar (same image, entrypoint overridden to
# /usr/local/bin/foundry-egress-gate) — reusing one image for both roles is
# why this stays a single lineage rather than a second Dockerfile.
#
# Build context is the repo root (see deploy/docker-compose.yaml's `dev`
# service for the same "context: .." pattern) so the builder stage can
# compile the gate binary from this repo's own source.


# Base image pinned by digest (not just tag) — this specific image is the
# isolation boundary itself (governing doc N8/13.4: "signed base images
# pinned by digest"), so it carries a higher bar than deploy/Dockerfile.dev
# (Task 1, unpinned tag, out of this task's scope to change). Digest
# resolved from the golang:1.25.7 manifest LIST (multi-arch image index),
# not a single-platform manifest, so this still resolves correctly on both
# amd64 and arm64 runners — re-resolve if 1.25.7 is ever bumped:
#   docker buildx imagetools inspect golang:1.25.7 | grep ^Digest
ARG GOLANG_DIGEST=sha256:5a79b94c34c299ac0361fbb7c7fca6dc552e166b42341050323fa3ab137d7be9

FROM golang:1.25.7@${GOLANG_DIGEST} AS gatebuild
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/foundry-egress-gate ./internal/executor/sandbox/gate

FROM golang:1.25.7@${GOLANG_DIGEST} AS runtime

ARG USER_UID=10001
ARG USER_GID=10001

# go/node/git/make/bash: the "task tooling" the card's Steps name. No
# Playwright, no Docker CLI, no extra package managers — unlike
# deploy/Dockerfile.dev (Task 1), this image is the isolation *boundary*
# itself (governing doc N8/13.4: "no Docker socket", "read-only base
# image"), so it deliberately carries the smallest tool surface that lets
# go/node-based task commands run, not everything `dev` happens to have.
#
# Node.js install avoids deploy/Dockerfile.dev's `curl | bash` pattern
# deliberately (same "higher bar" reasoning as the digest pin above): this
# fetches only NodeSource's GPG signing key over HTTPS and imports it into
# an apt keyring, then writes a standard apt source pinned to that key —
# nodejs itself is installed via `apt-get install`, verified against that
# key by apt like any other signed package, never by executing a
# downloaded script.
RUN apt-get update && apt-get dist-upgrade -y && apt-get install -y --no-install-recommends \
    make \
    git \
    bash \
    ca-certificates \
    curl \
    gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && chmod 644 /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_20.x nodistro main" \
    > /etc/apt/sources.list.d/nodesource.list \
    && apt-get update && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY --from=gatebuild /out/foundry-egress-gate /usr/local/bin/foundry-egress-gate

RUN mkdir -p /etc/foundry && \
    groupadd --gid ${USER_GID} sandbox \
    && useradd --uid ${USER_UID} --gid ${USER_GID} --create-home --shell /bin/bash sandbox

# Runtime-stage default user is non-root; internal/executor/sandbox's
# buildSandboxRunArgs/buildGateRunArgs still pass --user explicitly on every
# `run` invocation (defense in depth — the image default must not be relied
# on alone as the only place root is excluded).
USER sandbox
WORKDIR /workspace
