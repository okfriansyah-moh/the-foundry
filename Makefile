COMPOSE := docker compose -f deploy/docker-compose.yaml
RUN := $(COMPOSE) run --rm dev

.PHONY: bootstrap up down doctor test lint fitness fitness-tenx fitness-selftest doclint skp-e2e skp-resume e2e-github e2e-bitbucket e2e-venture e2e-tenx evidence-verify projection-rebuild plan-run migrate-up migrate-down migrate-status bench-baseline bench-foundry drill-brownout backup restore drill-backup-restore m1-exit chaos soak-fairness alerts-drill redteam dr-drill soak-telegram release-dryrun upgrade-drill soak-72h soak-learning

bootstrap:
	$(COMPOSE) build dev
	$(RUN) go mod download

up:
# Task 31 (FND-12): PROFILE=obs also brings up prometheus+grafana
# (deploy/docker-compose.yaml's `obs` compose profile); plain `make up`
# is unchanged.
ifeq ($(PROFILE),obs)
	COMPOSE_PROFILES=obs $(COMPOSE) up -d postgres temporal prometheus grafana
else
	$(COMPOSE) up -d postgres temporal
endif

down:
ifeq ($(PROFILE),obs)
ifdef KEEP_DATA
	COMPOSE_PROFILES=obs $(COMPOSE) down
else
	COMPOSE_PROFILES=obs $(COMPOSE) down -v
endif
else
ifdef KEEP_DATA
	$(COMPOSE) down
else
	$(COMPOSE) down -v
endif
endif

doctor:
	@command -v docker >/dev/null 2>&1 || { echo "Docker not found. Install: https://docs.docker.com/get-docker/"; exit 1; }
	@docker compose version >/dev/null 2>&1 || { echo "Docker Compose v2 not found. Install: https://docs.docker.com/compose/install/"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "Docker daemon not reachable. Start Docker Desktop/Engine: https://docs.docker.com/get-docker/"; exit 1; }
	$(RUN) go run ./cmd/foundry doctor

test:
	$(RUN) go test ./...

lint:
	$(RUN) golangci-lint run

fitness:
	$(RUN) bash scripts/fitness.sh

fitness-tenx:
	$(RUN) bash scripts/check_tenx_prohibition.sh .

# docs/PLAN.md Task 18 (SKP-16): proves every test/fitness_seeds/* fixture
# actually fails its corresponding fitness check.
fitness-selftest:
	$(RUN) bash scripts/fitness_selftest.sh

# docs/PLAN.md Task 37 (FND-18): the documentation-governance subset of
# fitness, callable standalone so CI can gate PRs that touch docs without
# waiting on the full suite. `make fitness` step (d) calls this exact same
# script (scripts/doclint/run.sh) — one implementation, never two.
doclint:
	$(RUN) bash scripts/doclint/run.sh

# docs/PLAN.md Task 19 (SKP-17): M0 exit proof. doctor -> three plans
# (success, deterministic-fail, resume) -> evidence verify -> status
# consistency check -> fitness -> archive evidence/history ->
# docs/notes/m0-exit-report.md. See test/skp_e2e.sh for the full sequence.
skp-e2e:
# --use-aliases (Task 31 / FND-12): registers this run container under the
# `dev` service's own network alias so `make up PROFILE=obs`'s prometheus
# service (deploy/prometheus/prometheus.yml, scrape target `dev:9090`) can
# actually resolve and scrape foundryd's /metrics while this script runs it
# — verified manually via `docker compose run --use-aliases --service-ports
# dev ...` + `curl localhost:9091/api/v1/targets` showing health "up". Not
# added to every other target's $(RUN) to avoid widening blast radius past
# this task's own Acceptance scenario.
	$(COMPOSE) run --rm --use-aliases dev bash test/skp_e2e.sh

# docs/PLAN.md Task 16 (SKP-14): runs the forced-restart resume proof 20
# times in a row; a single failed run fails the whole target (any single
# failure is red overall, per the task's Acceptance).
skp-resume:
	$(RUN) bash -c 'for i in $$(seq 20); do echo "=== skp-resume attempt $$i/20 ==="; bash test/skp_resume_test.sh || exit 1; done'

# docs/PLAN.md Task 20 (FND-01): goose-backed migration tooling. All
# schema evolution goes through internal/db/migrations (embedded) via
# cmd/foundry migrate; no ORM.
migrate-up:
	$(RUN) go run ./cmd/foundry migrate up

migrate-down:
	$(RUN) go run ./cmd/foundry migrate down

migrate-status:
	$(RUN) go run ./cmd/foundry migrate status

# docs/PLAN.md Task 134 (ACC-01): mine ≥3 control-arm deliveries from git
# history (B12) and write benchmarks/baseline/** run records + report.
bench-baseline:
	$(RUN) go run ./cmd/foundry bench baseline

# docs/PLAN.md Task 135 (ACC-02): compare Foundry arm against recorded baseline.
bench-foundry:
	$(RUN) go run ./cmd/foundry bench foundry

e2e-github:
	$(RUN) bash test/e2e_github.sh

# docs/PLAN.md Task 137 (TX-11): Bitbucket write parity. Local bare-repo
# contract always runs; real bitbucket.org is gated behind RUN_BITBUCKET_LIVE=1
# plus SCM_WRITE_TEST_BITBUCKET_* (never auto-run against production remotes).
e2e-bitbucket:
	$(RUN) go test ./internal/scm/write/... -count=1 -race -run 'Bitbucket|BackendContract'

e2e-venture:
	$(RUN) bash test/e2e/venture/run.sh

e2e-tenx:
	$(RUN) bash test/e2e/tenx/run.sh

evidence-verify:
	@echo "not yet: evidence-verify" && exit 1

# docs/PLAN.md Task 39 (FND-20) M1-exit Acceptance's "projection rebuild"
# bullet: `foundry projection rebuild`'s own round-trip proof (Task 14's
# drop-table -> rebuild -> identical-checksum contract, plus the
# out-of-order/duplicate-seq idempotency guard). Task 38's rollout e2e
# (test/projection_rollout_e2e.sh) is the separate, additional versioned-
# rollout proof and stays its own explicit `bash` invocation rather than
# being folded into this target's name.
projection-rebuild:
	$(RUN) bash test/projection_rebuild_e2e.sh

plan-run:
	$(RUN) go run ./tools/planrunner --plan=docs/PLAN.md

# docs/PLAN.md Task 33 (FND-14): proves brownout mode sheds the learning
# lane first while recovery/delivery/notification keep draining, and that
# a poisoned work item's dead-letter record fires a real, deliverable P1
# alert. See test/drill/brownout/main.go for the full scenario; no live
# Temporal/Postgres/network dependency.
drill-brownout:
	$(RUN) go run ./test/drill/brownout

# docs/PLAN.md Task 39 (FND-20) M1 exit: pg_dump (custom format) of the
# Foundry Postgres database + tar.gz of the evidence store, into a
# timestamped, checksummed backups/<ts>/ directory (gitignored — runtime
# artifact, not source). See scripts/backup.sh's own doc comment.
backup:
	$(RUN) bash scripts/backup.sh

# docs/PLAN.md Task 39 (FND-20) M1 exit: restores the given (or, if
# BACKUP_DIR is unset, the most recent) backups/<ts>/ directory into a
# SCRATCH Postgres database + scratch evidence dir — never the live
# `foundry` database — and verifies data integrity (row counts, file
# checksums, audit chain) rather than trusting a bare exit code. See
# scripts/restore.sh's own doc comment.
restore:
	$(RUN) bash scripts/restore.sh $(BACKUP_DIR)

# docs/PLAN.md Task 39 (FND-20) M1 exit Steps: "run a plan -> backup
# mid-flight -> destroy env -> restore -> workflow continues". Runs
# entirely against an isolated `foundry_drill` database (never the shared
# `foundry` database this environment's other validation steps depend on)
# so its real `DROP DATABASE` never disturbs the rest of `make m1-exit`.
# See test/drill/backup_restore_e2e.sh's own doc comments for exactly what
# "workflow continues" means here and why.
drill-backup-restore:
	$(RUN) bash test/drill/backup_restore_e2e.sh

# docs/PLAN.md Task 39 (FND-20): M1 exit meta-target chaining every M1
# Acceptance bullet plus this task's own backup/restore drill. Each
# sub-target is this same task's Exec role (integration) composing
# already-existing, independently-owned proofs — none reimplemented here:
#   - e2e-github            Task 27's kernel-only-push proof
#   - approval_stepup_e2e   Task 25's WebAuthn step-up gate e2e
#   - notify soak           Task 30's 5k-event Telegram soak harness
#   - projection-rebuild    Task 14's rebuild round-trip (this task's own
#                           new Make target, wiring the pre-existing script)
#   - audit verify          this task's own `foundry audit verify` CLI
#                           (writer: Task 20/24's AppendAuditRow) against
#                           the live `foundry` database
#   - drill-brownout        Task 33's shed-order + DLQ-alert drill
#   - drill-backup-restore  this task's own backup/restore/destroy drill
# A single failing step fails the whole target (no partial "M1 exit").
m1-exit:
	$(MAKE) e2e-github
	$(RUN) bash test/approval_stepup_e2e.sh
	$(RUN) go run ./test/soak/telegram
	$(MAKE) projection-rebuild
	$(RUN) go run ./cmd/foundry audit verify
	$(MAKE) drill-brownout
	$(MAKE) drill-backup-restore

chaos:
	$(RUN) go test -tags chaos ./test/chaos/...

soak-fairness:
	$(RUN) go run ./test/soak/fairness

soak-learning:
	$(RUN) go run ./test/soak/learning

alerts-drill:
	$(RUN) go test ./test/... -run TestAlertsConformance -short

redteam:
	$(RUN) go test -tags redteam ./test/redteam/...

dr-drill:
	$(RUN) bash test/drill/dr_drill.sh

soak-telegram:
	$(RUN) go run ./test/soak/telegram

release-dryrun:
	$(RUN) bash -c 'command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser not installed"; exit 1; }; goreleaser release --snapshot --skip-publish --clean'

upgrade-drill:
	$(RUN) bash test/drill/upgrade_drill.sh

soak-72h:
	$(RUN) bash -c 'echo "72h unattended soak is staging-gated; run track fixtures in a long-lived environment"'
