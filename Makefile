COMPOSE := docker compose -f deploy/docker-compose.yaml
RUN := $(COMPOSE) run --rm dev

.PHONY: bootstrap up down doctor test lint fitness fitness-selftest skp-e2e skp-resume e2e-github e2e-venture e2e-tenx evidence-verify projection-rebuild plan-run migrate-up migrate-down migrate-status

bootstrap:
	$(COMPOSE) build dev
	$(RUN) go mod download

up:
	$(COMPOSE) up -d postgres temporal

down:
ifdef KEEP_DATA
	$(COMPOSE) down
else
	$(COMPOSE) down -v
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

# docs/PLAN.md Task 18 (SKP-16): proves every test/fitness_seeds/* fixture
# actually fails its corresponding fitness check.
fitness-selftest:
	$(RUN) bash scripts/fitness_selftest.sh

# docs/PLAN.md Task 19 (SKP-17): M0 exit proof. doctor -> three plans
# (success, deterministic-fail, resume) -> evidence verify -> status
# consistency check -> fitness -> archive evidence/history ->
# docs/notes/m0-exit-report.md. See test/skp_e2e.sh for the full sequence.
skp-e2e:
	$(RUN) bash test/skp_e2e.sh

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

e2e-github:
	@echo "not yet: e2e-github" && exit 1

e2e-venture:
	@echo "not yet: e2e-venture" && exit 1

e2e-tenx:
	@echo "not yet: e2e-tenx" && exit 1

evidence-verify:
	@echo "not yet: evidence-verify" && exit 1

projection-rebuild:
	@echo "not yet: projection-rebuild" && exit 1

plan-run:
	$(RUN) go run ./tools/planrunner --plan=docs/PLAN.md
