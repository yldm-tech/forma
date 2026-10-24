# Orchestrates the Go server and the TypeScript workspace. Every target runs from the repo root.
# The TypeScript side runs through Vite+ (`vp`, installed by `pnpm install`); Go comes from apps/server/go.mod.

SERVER := apps/server
WEB_DIST := apps/web/dist
EMBED_DIR := $(SERVER)/internal/webui/dist
BIN := bin/forma
# The golangci-lint release CI runs (.github/workflows/pr.yml); `make lint` refuses any other, since linters and their defaults change between minor releases.
GOLANGCI_LINT_VERSION := 2.14.0

.PHONY: help deps-up deps-down authz-schema migrate seed seed-clear lint-migrations e2e dev-server dev-worker dev-web build web embed server generate lint test check check-generated clean

help:
	@echo "deps-up      start local dependencies (docker compose), write the SpiceDB schema and create the storage bucket"
	@echo "deps-down    stop local dependencies"
	@echo "authz-schema write apps/server/authzed/schema.zed into SpiceDB (reads .env, else .env.example)"
	@echo "migrate      apply pending database and job queue migrations (reads .env)"
	@echo "seed         write the demo organization, users, surveys and responses into the .env database (forma seed)"
	@echo "seed-clear   remove what an earlier seed wrote, then seed again"
	@echo "dev-server   run the Go server against .env"
	@echo "dev-worker   run the background job worker against .env"
	@echo "dev-web      run the Vite dev server (proxies the API to the Go server)"
	@echo "build        build the SPA, embed it, and compile bin/forma"
	@echo "generate     regenerate wire_gen.go"
	@echo "check-generated  fail when wire_gen.go, routeTree.gen.ts or the exported JSON Schemas are stale"
	@echo "lint         golangci-lint $(GOLANGCI_LINT_VERSION) + vp check (format, Oxlint, type-check)"
	@echo "lint-migrations  Squawk on the migrations this branch adds or changes (against origin/main)"
	@echo "e2e          the Playwright browser suite against a running server (E2E_BASE_URL, default http://localhost:3000)"
	@echo "test         go test + workspace tests"
	@echo "check        the lint, test and generated-file checks CI runs (the Docker and Helm validation, the Go coverage floor and e2e run only in CI)"

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
	cd $(SERVER) && go build -o ../../$(BIN) ./cmd/forma
	set -a && . ./$$([ -f .env ] && echo .env || echo .env.example) && set +a && \
	for attempt in $$(seq 1 $(AUTHZ_SCHEMA_ATTEMPTS)); do \
		if ./$(BIN) authz schema apply; then exit 0; fi; \
		echo "SpiceDB is not ready for a schema write yet (attempt $$attempt/$(AUTHZ_SCHEMA_ATTEMPTS)); retrying in $(AUTHZ_SCHEMA_RETRY_DELAY)s." >&2; \
		sleep $(AUTHZ_SCHEMA_RETRY_DELAY); \
	done; \
	echo "Failed to write the SpiceDB schema; every permission check fails until it is written." >&2; exit 1

migrate:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma migrate

# The seed refuses to run without ALLOW_SEED=1, since it writes users with a published password. These targets set it: they only ever point at the local database in .env.
seed:
	set -a && . ./.env && set +a && cd $(SERVER) && ALLOW_SEED=1 go run ./cmd/forma seed

seed-clear:
	set -a && . ./.env && set +a && cd $(SERVER) && ALLOW_SEED=1 go run ./cmd/forma seed --clear

dev-server:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma serve

dev-worker:
	set -a && . ./.env && set +a && cd $(SERVER) && go run ./cmd/forma worker

dev-web:
	pnpm exec vp run --filter @forma/web dev

# A recipe rather than three prerequisites, so `make -j build` cannot embed or compile before the SPA is built. embed and server keep no prerequisites of their own: e2e.yml builds the workspace itself and then runs `make embed server`.
build:
	$(MAKE) web
	$(MAKE) embed
	$(MAKE) server

# Builds every workspace package in dependency order; web depends on the SDK packages whose bundles it ships.
web:
	pnpm exec vp run -r build

# The SPA is embedded with go:embed, which cannot reach outside the Go module, so the build is copied in. The placeholder keeps the embed pattern valid in a fresh checkout.
embed:
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
	cp -R $(WEB_DIST)/. $(EMBED_DIR)/

server:
	cd $(SERVER) && CGO_ENABLED=0 go build -trimpath -o ../../$(BIN) ./cmd/forma

generate:
	cd $(SERVER) && go tool wire ./internal/app

lint: web
	@found=$$(golangci-lint version --short 2>/dev/null); \
	if [ "$$found" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "make lint needs golangci-lint $(GOLANGCI_LINT_VERSION), the version CI runs; found $${found:-none} on PATH." >&2; exit 1; \
	fi
	cd $(SERVER) && golangci-lint run ./...
	pnpm exec vp check

lint-migrations:
	BASE_REF=$$(git merge-base HEAD origin/main) bash tooling/scripts/lint-migrations.sh

e2e:
	pnpm exec vp run --filter @forma/e2e e2e

test: web
	cd $(SERVER) && go test -race ./...
	pnpm exec vp run -r test

# The committed generated files CI checks. wire_gen.go is compared with the copy present before regenerating, so uncommitted edits elsewhere do not matter; a stale one is left regenerated for you to commit. routeTree.gen.ts is regenerated by the web build and compared with the git index, so stage route changes before running this.
check-generated: web
	@before=$$(mktemp) && trap 'rm -f "$$before"' EXIT && \
	cp $(SERVER)/internal/app/wire_gen.go "$$before" && \
	(cd $(SERVER) && go tool wire ./internal/app) && \
	if ! diff -u "$$before" $(SERVER)/internal/app/wire_gen.go; then \
		echo "wire_gen.go was stale and has been regenerated; commit it." >&2; exit 1; \
	fi
	git diff --exit-code -- apps/web/src/routeTree.gen.ts
	pnpm exec vp run --filter @forma/types export-json-schema -- --check

check: lint test check-generated lint-migrations
	pnpm exec vp run catalog:check
	pnpm exec vp run tests:run-check
	pnpm exec vp run bundle:check
	pnpm exec vp run scan-translations
	pnpm exec vp run authzed:validate

clean:
	rm -rf bin $(WEB_DIST)
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
