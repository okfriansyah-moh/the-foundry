COMPOSE := docker compose -f deploy/docker-compose.yaml
RUN := $(COMPOSE) run --rm dev

.PHONY: bootstrap up down doctor test lint fitness skp-e2e e2e-github e2e-venture e2e-tenx evidence-verify projection-rebuild

bootstrap:
	$(COMPOSE) build dev
	$(RUN) go mod download

up:
	@echo "not yet: up" && exit 1

down:
	@echo "not yet: down" && exit 1

doctor:
	@echo "not yet: doctor" && exit 1

test:
	$(RUN) go test ./...

lint:
	$(RUN) golangci-lint run

fitness:
	$(RUN) bash scripts/fitness.sh

skp-e2e:
	@echo "not yet: skp-e2e" && exit 1

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
