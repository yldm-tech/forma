# Orchestrates the Go server and the TypeScript workspace. Every target runs from the repo root.
# The TypeScript side runs through Vite+ (`vp`, installed by `pnpm install`); Go comes from server/go.mod.

SERVER := server
WEB_DIST := web/dist
EMBED_DIR := $(SERVER)/internal/webui/dist
BIN := bin/forma

.PHONY: help deps-up deps-down migrate dev-server dev-worker dev-web build web embed server generate lint test check clean

help:
	@echo "deps-up      start local dependencies (docker compose)"
	@echo "deps-down    stop local dependencies"
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

deps-down:
	docker compose -f docker-compose.dev.yml down

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
