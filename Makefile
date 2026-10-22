# Orchestrates the Go server and the TypeScript workspace. Every target runs from the repo root.
# The TypeScript side runs through Vite+ (`vp`, installed by `pnpm install`); Go comes from server/go.mod.

SERVER := server
WEB_DIST := web/dist
EMBED_DIR := $(SERVER)/internal/webui/dist
BIN := bin/forma

.PHONY: help deps-up deps-down authz-schema migrate dev-server dev-worker dev-web build web embed server generate lint test check clean

help:
	@echo "deps-up      start local dependencies (docker compose), write the SpiceDB schema and create the storage bucket"
	@echo "deps-down    stop local dependencies"
	@echo "authz-schema write authzed/schema.zed into SpiceDB (reads .env, else .env.example)"
	@echo "migrate      apply pending database and job queue migrations (reads .env)"
	@echo "dev-server   run the Go server against .env"
	@echo "dev-worker   run the background job worker against .env"
	@echo "dev-web      run the Vite dev server (proxies the API to the Go server)"
	@echo "build        build the SPA, embed it, and compile bin/forma"
	@echo "generate     regenerate wire_gen.go"
	@echo "lint         golangci-lint + vp check (format, Oxlint, type-check)"
	@echo "test         go test + workspace tests"
	@echo "check        everything CI runs"

deps-up:
	docker compose -f docker-compose.dev.yml up -d --wait
	$(MAKE) authz-schema
	docker compose -f docker-compose.dev.yml run --rm rustfs-init

deps-down:
	docker compose -f docker-compose.dev.yml down

# docker compose only migrates SpiceDB's datastore; until the schema is written every permission check fails with authzed_failed_precondition. SpiceDB reports healthy a moment before it accepts a schema write, so the write itself is retried. An unchanged schema is a no-op, so this is safe to re-run; a SpiceDB holding a different schema is left alone (pass its digest with `forma authz schema apply --expected-current-digest=...`).
AUTHZ_SCHEMA_ATTEMPTS ?= 10
AUTHZ_SCHEMA_RETRY_DELAY ?= 3

authz-schema:
	cd $(SERVER) && go build -o ../$(BIN) ./cmd/forma
	set -a && . ./$$([ -f .env ] && echo .env || echo .env.example) && set +a && \
	for attempt in $$(seq 1 $(AUTHZ_SCHEMA_ATTEMPTS)); do \
		if ./$(BIN) authz schema apply; then exit 0; fi; \
		echo "SpiceDB is not ready for a schema write yet (attempt $$attempt/$(AUTHZ_SCHEMA_ATTEMPTS)); retrying in $(AUTHZ_SCHEMA_RETRY_DELAY)s." >&2; \
		sleep $(AUTHZ_SCHEMA_RETRY_DELAY); \
	done; \
	echo "Failed to write the SpiceDB schema; every permission check fails until it is written." >&2; exit 1

migrate:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma migrate

dev-server:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma serve

dev-worker:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma worker

dev-web:
	pnpm exec vp run --filter @forma/web dev

build: web embed server

# Builds every workspace package in dependency order; web depends on the SDK packages whose bundles it ships.
web:
	pnpm exec vp run -r build

# The SPA is embedded with go:embed, which cannot reach outside the Go module, so the build is copied in. The placeholder keeps the embed pattern valid in a fresh checkout.
embed:
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
	cp -R $(WEB_DIST)/. $(EMBED_DIR)/

server:
	cd $(SERVER) && CGO_ENABLED=0 go build -trimpath -o ../$(BIN) ./cmd/forma

generate:
	cd $(SERVER) && go tool wire ./internal/app

lint: web
	cd $(SERVER) && golangci-lint run ./...
	pnpm exec vp check

test: web
	cd $(SERVER) && go test -race ./...
	pnpm exec vp run -r test

check: lint test
	pnpm exec vp run catalog:check
	pnpm exec vp run api:v3:lint
	pnpm exec vp run authzed:validate

clean:
	rm -rf bin $(WEB_DIST)
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
