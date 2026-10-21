# Repository Guidelines

## Status: rewrite in progress

Forma is being rewritten from a Next.js monolith (the `yldm-tech/forma2` repository, whose history this repository continues) into a Go server plus a TanStack single-page app. The rewrite moves module by module; until a module lands here, its behaviour is defined by forma2's `main`. When porting a module, read the forma2 implementation and its tests first: the tests are the behaviour checklist, and every one that still applies becomes a Go or Vitest test here.

Phases, in order: P0 foundation (this skeleton, the new schema, recorded contract tests), P1 identity and tenancy, P2 survey definition, P3 response collection, P4 everything after a response, P5 external APIs and operations, P6 data migration and cutover. Every phase lands as a series of PRs scoped to one module each.

## Project Structure

| Path                                                          | What it is                                                                                                                                  |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `server/`                                                     | Go module `github.com/yldm-tech/forma/server`: gin, wire, GORM. One binary, `forma`, whose subcommands are the processes (`serve` today).   |
| `server/internal/platform/`                                   | Cross-cutting infrastructure: config, logger, database, HTTP server and middleware.                                                         |
| `server/internal/<feature>/`                                  | One package per feature, layered `handler` → `service` → `repository`. Authorization is checked once, in the service layer.                 |
| `server/internal/app/`                                        | Composition root. `wire.go` declares the graph, `wire_gen.go` is generated and committed.                                                   |
| `server/internal/webui/`                                      | Serves the SPA build embedded with `go:embed`, plus the SDK bundles under `/js/*`.                                                          |
| `web/`                                                        | The admin SPA: Vite, React 19, TanStack Router (file-based, `src/routes/`), TanStack Query, Tailwind 4.                                     |
| `packages/surveys`                                            | Preact survey runtime, shipped as `/js/surveys.umd.cjs` and rendered on customer sites and the public survey pages. Carried over unchanged. |
| `packages/js-core`                                            | The customer-site SDK, shipped as `/js/forma.umd.cjs`. Carried over unchanged.                                                              |
| `packages/survey-ui`, `packages/types`, `packages/i18n-utils` | Shared UI, zod types and i18n helpers used by the packages above.                                                                           |
| `apps/storybook`                                              | Storybook for `packages/survey-ui`.                                                                                                         |
| `authzed/`                                                    | SpiceDB schema (`schema.zed`) and its assertion suite.                                                                                      |
| `docs/`                                                       | Product and API documentation (Mintlify). `docs/api-v3-reference` is the source of the v3 OpenAPI contract.                                 |

## Commands

Everything runs from the repository root. The TypeScript side uses [Vite+](https://viteplus.dev/) (`vp`): Vite (Rolldown) for dev and build, Vitest for tests, Oxlint and Oxfmt for lint and format, tsgolint for type checks, and `vp run` as the workspace task runner. There is no ESLint, Prettier or Turborepo. `pnpm` is the version pinned in `package.json` (`packageManager`) and Node.js the one in `.node-version`; Go comes from `server/go.mod`.

- `make deps-up` / `make deps-down` — start or stop local dependencies (`docker-compose.dev.yml`).
- `cp .env.example .env`, then `make dev-server` — run the Go server on `:3000`.
- `make dev-web` — run the Vite dev server on `:5173`; it proxies `/api` and `/health` to the Go server.
- `make build` — build the SPA (and the SDK bundles it ships), embed it, and compile `bin/forma`.
- `make generate` — regenerate `server/internal/app/wire_gen.go` after changing a provider.
- `make check` — what CI runs: golangci-lint, `go test -race`, `vp check` (Oxfmt, Oxlint, type-check), the Vitest suites, the catalog and OpenAPI checks, and the SpiceDB schema assertions.
- `pnpm exec vp check --fix` — format and apply lint autofixes. A pre-commit hook (`.vite-hooks/pre-commit`, installed by `pnpm install`) runs this on staged files.
- `pnpm exec vp run --filter @forma/surveys test -- src/lib/logic.test.ts` — one TypeScript test file; `cd server && go test ./internal/health -run TestReady` — one Go test.

## Go conventions

- Use mature libraries for protocols, parsing and serialization; write only business logic by hand. A new module dependency needs agreement before it is added.
- Keep the simplest shape that works: a function before an interface, an interface only where there are two implementations or a test seam.
- Validate at system boundaries (HTTP handlers, queue consumers, config). Do not wrap internal calls in defensive error handling.
- Errors are wrapped with context (`fmt.Errorf("open database: %w", err)`) and logged once, where they are handled.
- Every process reads configuration once, at startup, through `internal/platform/config`. A missing or malformed variable fails the start.
- Logs are structured (`log/slog`). Never log secrets, tokens, raw request bodies, or raw URL paths that can carry a token.
- `golangci-lint` (config in `server/.golangci.yml`) and `gofmt`/`goimports` are enforced in CI.

## API contracts

- Paths and payloads that something outside this repository calls are frozen: the client API used by `packages/js-core` and `packages/surveys` (`/api/v1/client/*`, `/api/v2/client/*`), `/js/*`, the v3 management API, MCP, OAuth and webhook callback URLs, and `/health`, `/health/ready`. Reproduce forma2's behaviour byte for byte and prove it with a contract test.
- `/api/v3` is the forward API and is specified first in `docs/api-v3-reference`; the Go server and the SPA's client are generated from that spec. Endpoints only the admin UI needs live under `/api/app/*`.

## TypeScript workspace

### Shared dependency versions (pnpm catalog)

Every dependency used by two or more workspaces is pinned once in the `catalog:` block of `pnpm-workspace.yaml`, and each `package.json` references it as `"catalog:"`. Bumping a shared dependency means editing the catalog entry, never a `package.json`. `pnpm check` runs `scripts/check-catalog.mjs`, which fails on a literal version for a catalogued name and on an uncatalogued dependency shared by two workspaces. A peer dependency range is exempt.

### SDK bundles and stale builds

`packages/surveys` and `packages/js-core` are consumed through their builds. Building them copies the UMD bundles into `web/public/js/`, Vite copies that into the SPA build, and the Go binary embeds and serves it at `/js/*` with the cache and CORS headers forma2 sent. `web` declares both packages as dev dependencies, so `vp run -r build` (`pnpm build`, `make build`) builds them first.

Running `vp test` directly inside a package bypasses that graph, and so does `vp check`: its type-aware rules resolve sibling packages through their `dist/` declarations. After a branch switch, `dist/` can be older than `src/`, and the symptoms (a missing export, `instanceof` on `undefined`, failures in files you never touched) look like real regressions. Rebuild with `pnpm build` before believing them.

### Tailwind and package CSS

Tailwind v4 detects sources from the consuming app's own root and never descends into `node_modules`, where every `@forma/*` package is linked. Packages therefore ship their own CSS: `@forma/survey-ui` exports `./styles`, `@forma/surveys` inlines its styles into the bundle. Only two JS Tailwind configs remain (`packages/survey-ui/tailwind.config.ts`, `packages/surveys/tailwind.config.cjs`), both reached through an explicit `@config` from the package's stylesheet.

### Frontend conventions

- Routes are files under `web/src/routes/`; `web/src/routeTree.gen.ts` is generated by the router plugin during `vite build`/`vite dev` and committed. CI fails if it is stale.
- Server state goes through TanStack Query with `queryOptions` factories; route loaders prefetch through the same options. Do not keep a second copy of server data in component state.
- Avoid `any` (`typescript/no-explicit-any` is an error), keep components focused, and meet basic accessibility.
- Lint and format rules live only in the root `vite.config.ts`; package configs carry build, test and dev settings. Per-path exceptions go in `lint.overrides` with a comment saying why.

## Testing

- Test behaviour and outcomes at the cheapest level that can fail on them.
- Go: table-driven tests next to the code; integration tests that need Postgres, Valkey or SpiceDB start real ones with testcontainers.
- TypeScript: Vitest next to the code.
- A ported module is done when forma2's tests for it are reproduced here and pass.

## GitHub Actions

- Always set minimal `permissions` for `GITHUB_TOKEN`.
- On `ubuntu-latest`, add `step-security/harden-runner` as the first step.
- Pin every third-party action to a full commit SHA with the version in a trailing comment.

## Commit & Pull Request Guidelines

Commits follow Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`, `test:`, `docs:`). Keep each PR to one module, use `.github/pull_request_template.md`, and record migrations or environment changes in it. Do not restate what CI already reports.
