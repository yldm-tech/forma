# Repository Guidelines

## Status: rewrite in progress

Forma is being rewritten from a Next.js monolith (the `yldm-tech/forma2` repository, whose history this repository continues) into a Go server plus a TanStack single-page app. The rewrite moves module by module; until a module lands here, its behaviour is defined by forma2's `main`. When porting a module, read the forma2 implementation and its tests first: the tests are the behaviour checklist, and every one that still applies becomes a Go or Vitest test here.

Phases, in order: P0 foundation (this skeleton, the new schema, recorded contract tests), P1 identity and tenancy, P2 survey definition, P3 response collection, P4 everything after a response, P5 external APIs and operations, P6 data migration and cutover. Every phase lands as a series of PRs scoped to one module each.

## Project Structure

| Path                                                          | What it is                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apps/server/`                                                | Go module `github.com/yldm-tech/forma/apps/server`: gin, wire, GORM. One binary, `forma`, whose subcommands are the processes (`serve`, `worker`, `migrate`, and `start` for the container).                                                                                                                                                                                                        |
| `apps/server/internal/platform/`                              | Cross-cutting infrastructure: config, logger, database, HTTP server, the `/api/app` conventions (`httpapi`, `apperr`), Valkey, rate limits, mail, audit events and the River job queue (`jobs`).                                                                                                                                                                                                    |
| `apps/server/internal/<feature>/`                             | One package per feature, layered `handler` → `service` → `repository`. Authorization is checked once, in the service layer.                                                                                                                                                                                                                                                                         |
| `apps/server/internal/app/`                                   | Composition root. `wire.go` declares the graph, `wire_gen.go` is generated and committed.                                                                                                                                                                                                                                                                                                           |
| `apps/server/internal/webui/`                                 | Serves the SPA build embedded with `go:embed` under `/app` (`config.AppBasePath`), plus the SDK bundles under `/js/*` and the other root-URL files of `apps/web/public`; the admin pages' old root paths redirect to `/app`.                                                                                                                                                                        |
| `apps/web/`                                                   | The admin SPA: Vite, React 19, TanStack Router (file-based, `src/routes/`), TanStack Query, Tailwind 4. A second entry, `survey.html` with `src/survey-page/`, is the public survey page bundle the Go server fills for `/s`, `/c` and `/p` (`apps/server/internal/linksurvey`).                                                                                                                    |
| `packages/surveys`                                            | Preact survey runtime, shipped as `/js/surveys.umd.cjs` and rendered on customer sites and the public survey pages. Carried over unchanged.                                                                                                                                                                                                                                                         |
| `packages/js-core`                                            | The customer-site SDK, shipped as `/js/forma.umd.cjs`. Carried over unchanged.                                                                                                                                                                                                                                                                                                                      |
| `packages/survey-ui`, `packages/types`, `packages/i18n-utils` | Shared UI, zod types and i18n helpers used by the packages above.                                                                                                                                                                                                                                                                                                                                   |
| `apps/storybook`                                              | Storybook for `packages/survey-ui`.                                                                                                                                                                                                                                                                                                                                                                 |
| `apps/e2e`                                                    | The Playwright browser suite (`@forma/e2e`, `playwright.config.ts`), run against a running server by `make e2e`.                                                                                                                                                                                                                                                                                    |
| `deploy/docker/`, `deploy/helm/forma`                         | The production image (`deploy/docker/Dockerfile`: distroless, uid 1001, default command `forma start`, built with the repository root as context, `docker build -f deploy/docker/Dockerfile .`), the production compose stack (`deploy/docker/docker-compose.yml`) and the Helm chart; `docker-build-validation.yml` and `helm-chart-validation.yml` check them, `release.yml` publishes the image. |
| `apps/server/authzed/`                                        | SpiceDB schema (`schema.zed`) and its assertion suite; it is the schema's only copy, embedded by the `authzed` Go package and read from there by `internal/authz`.                                                                                                                                                                                                                                  |
| `tooling/`                                                    | Build tooling, not runtime code: `tooling/typescript` holds the shared `tsconfig` bases (`@forma/config-typescript`), `tooling/vite-plugins` the Vite and PostCSS plugins (`@forma/vite-plugins`), and `tooling/scripts` the repository checks and CI helpers (`check-catalog.mjs`, `ci-stack.sh`, ...).                                                                                            |

## Commands

Everything runs from the repository root. The TypeScript side uses [Vite+](https://viteplus.dev/) (`vp`): Vite (Rolldown) for dev and build, Vitest for tests, Oxlint and Oxfmt for lint and format, tsgolint for type checks, and `vp run` as the workspace task runner. There is no ESLint, Prettier or Turborepo. `pnpm` is the version pinned in `package.json` (`packageManager`) and Node.js the one in `.node-version`; Go comes from `apps/server/go.mod`, which the root `go.work` uses.

- `make deps-up` / `make deps-down` — start or stop local dependencies (`docker-compose.dev.yml`); `deps-up` also runs `make authz-schema`, which writes `apps/server/authzed/schema.zed` into the local SpiceDB.
- `forma authz schema check|apply`, `forma authz backfill [--apply] ...`, `forma authz outbox status|drain|replay` — operate the SpiceDB projection (`forma authz help`). Each prints one JSON line; exit 0 clean, 2 drift, 1 failed.
- `forma import-forma2 [--dry-run] [--source=<url>] [--refresh [--overwrite-changes]]` — copy a forma2 database (P6) into this schema in one transaction, verify it against forma2 read again, record it in `forma2_imports` and, with AuthZed on, run the authorization backfill; forma2's tables are only read. `forma start`, `serve` and `worker` refuse to start while forma2's `"User"` table has rows and `forma2_imports` has none, or while forma2's tables differ from the marks the latest record keeps (`forma2_import_drift()`, as after a rollback); `--discard-forma2-data --reason=...` records starting without forma2's data instead. `--refresh` refuses when this server changed the imported tables since the record; `--overwrite-changes` makes forma2's rows win. The mapping lives in `apps/server/internal/forma2import/tables.go` and the independent checks of its output in `expect.go`; its tests build forma2's schema from the Prisma migrations in `testdata/forma2-migrations` (3db326f).
- `cp .env.example .env`, `make migrate`, then `make dev-server` — apply the schema (goose, then River's job queue tables) and run the Go server on `:3000`. `make dev-worker` runs `forma worker`, the background job process.
- `make dev-web` — run the Vite dev server on `:5173`, the app at `http://localhost:5173/app/`; it proxies `/api` and `/health` to the Go server.
- `make build` — build the SPA (and the SDK bundles it ships), embed it, and compile `bin/forma`.
- `make generate` — regenerate `apps/server/internal/app/wire_gen.go` after changing a provider.
- `make check` — what CI runs: golangci-lint, `go test -race`, `vp check` (Oxfmt, Oxlint, type-check), the Vitest suites, the catalog checks, and the SpiceDB schema assertions.
- `pnpm exec vp check --fix` — format and apply lint autofixes. A pre-commit hook (`.vite-hooks/pre-commit`, installed by `pnpm install`) runs this on staged files.
- `pnpm exec vp run --filter @forma/surveys test -- src/lib/logic.test.ts` — one TypeScript test file; `cd apps/server && go test ./internal/health -run TestReady` — one Go test.

## Go conventions

- Use mature libraries for protocols, parsing and serialization; write only business logic by hand. A new module dependency needs agreement before it is added.
- Keep the simplest shape that works: a function before an interface, an interface only where there are two implementations or a test seam.
- Validate at system boundaries (HTTP handlers, queue consumers, config). Do not wrap internal calls in defensive error handling.
- Errors are wrapped with context (`fmt.Errorf("open database: %w", err)`) and logged once, where they are handled.
- Every process reads configuration once, at startup, through `internal/platform/config`. A missing or malformed variable fails the start. Boolean variables are forma2's `1`/`0` switches; any other value fails the start. The exceptions keep forma2's wider spellings: `AUTHZED_ENABLED` and `AUTHZED_INSECURE` (`config.AuthzedFlag`) also take `true`/`false`, and `SKIP_STARTUP_MIGRATION` takes `true`/`false`.
- Services return expected failures as `*apperr.Error` (forma2's error classes: status, stable code, message); anything else is an unexpected error that reaches the client as a generic 500. Handlers pass errors to `httpapi.Fail`, bind input with `httpapi.BindJSON` (validated through `binding` struct tags), and read the caller from `httpapi.ActorFrom` and `httpapi.ClientIP`.
- Authorization goes through `authz.Authorizer` (`Platform.Authz`): a service calls `Require(ctx, actor, action, resource)` (403 "Not authorized" on denial) or `Can`, with actions from forma2's vocabulary (`authz.WorkspaceWrite`, `authz.OrganizationManageAccess`, ...). PostgreSQL decides existence and tenancy first, then SpiceDB decides; an `*authz.Error` (SpiceDB unreachable, a revocation older than 60 s still undelivered) is never a denial and reaches the client as a 500. Services never write SpiceDB: the projection follows the `authz_projection_outbox` rows the triggers enqueue, delivered every five seconds by `forma worker`.
- Rate limits use `ratelimit.Limiter` with the rules in `ratelimit/rules.go` (forma2's namespaces and limits); they fail open when Valkey is unreachable. Audit events go through `audit.Emitter` with forma2's action and target names. Mail goes through `mailer.Mailer.SendTemplate`; a new email is a new `Content` type plus a template in `mailer/templates/`.
- Background work is a River job: register the worker (and any periodic schedule) in the `jobs.Registry` built for `forma worker` in `internal/app`, and enqueue inside the transaction of the change that causes it: run the change in `jobs.Enqueuer.Transaction` and enqueue with its `Tx.Enqueue`, River's `InsertTx` on the connection under GORM's transaction.
- Logs are structured (`log/slog`). Never log secrets, tokens, raw request bodies, or raw URL paths that can carry a token.
- `golangci-lint` (config in `apps/server/.golangci.yml`) and `gofmt`/`goimports` are enforced in CI.

## Database

- The schema is defined only by the goose SQL files in `apps/server/migrations/`, applied by `forma migrate` (it needs `DATABASE_URL` alone). GORM never runs `AutoMigrate`. A schema change is a new numbered file with both `Up` and `Down`; never edit one that has shipped.
- River's tables (`river_job`, `river_queue`, ...) are versioned by River itself; `forma migrate` applies them after the goose files and they never appear in `apps/server/migrations/`.
- PostgreSQL 18 or newer: primary keys default to `uuidv7()`. Closed value sets are `text` with a `CHECK`, not PostgreSQL enums.
- Rows migrated from forma2 get `uuidv5(<forma legacy namespace>, <old id>)` as their id, so an old id that arrives from outside (a survey link, an SDK snippet) is resolved by computing the same function. The namespace and the resolver live in `apps/server/internal/platform/legacyid` (`ID`, `Resolve`, `Matches`); use them rather than another copy, since the P6 data migration must derive the same ids.
- Every table that carries authorization data has a trigger that enqueues an `authz_projection_outbox` row in the same transaction; a new table of that kind needs one too, plus its projector and reconciliation in `apps/server/internal/authz`.
- JSON columns are validated in Go against JSON Schemas exported from the zod definitions in `packages/types` (`json-schema-registry.ts` lists them; `pnpm --filter @forma/types export-json-schema` regenerates `apps/server/internal/jsonschema/schemas/`, and CI fails when they are stale). The export keeps structure only: zod refinements such as unique ids or a translation's required `default` key are lost and must be ported as Go code in the service that owns the document. `apps/server/internal/jsonschema/testdata/parity.json` records cases on both sides of that line and is asserted by both a Vitest and a Go test.
- `apps/server/internal/platform/database/migrate_test.go` runs the migrations against a real PostgreSQL in Docker (testcontainers) and checks the constraints and triggers; `go test -short` skips it.

## API contracts

- Paths and payloads that something outside this repository calls are frozen: the client API used by `packages/js-core` and `packages/surveys` (`/api/v1/client/*`, `/api/v2/client/*`), `/js/*`, the v3 management API, MCP, OAuth and webhook callback URLs, and `/health`, `/health/ready`. Reproduce forma2's behaviour byte for byte and prove it with a contract test.
- `/api/v3` is the forward API. Endpoints only the admin UI needs live under `/api/app/*`: session-authenticated, errors as `{code, message, details}`, and every mutation must come from `WEBAPP_URL` or `PUBLIC_URL` (`Origin`) with an `X-Requested-With` header, which `apps/web/src/lib/api.ts` always sends.

## TypeScript workspace

### Shared dependency versions (pnpm catalog)

Every dependency used by two or more workspaces is pinned once in the `catalog:` block of `pnpm-workspace.yaml`, and each `package.json` references it as `"catalog:"`. Bumping a shared dependency means editing the catalog entry, never a `package.json`. `pnpm check` runs `tooling/scripts/check-catalog.mjs`, which fails on a literal version for a catalogued name and on an uncatalogued dependency shared by two workspaces. A peer dependency range is exempt.

### SDK bundles and stale builds

`packages/surveys` and `packages/js-core` are consumed through their builds. Building them copies the UMD bundles into `apps/web/public/js/`, Vite copies that into the SPA build, and the Go binary embeds and serves it at `/js/*` with the cache and CORS headers forma2 sent. `apps/web` declares both packages as dev dependencies, so `vp run -r build` (`pnpm build`, `make build`) builds them first.

Running `vp test` directly inside a package bypasses that graph, and so does `vp check`: its type-aware rules resolve sibling packages through their `dist/` declarations. After a branch switch, `dist/` can be older than `src/`, and the symptoms (a missing export, `instanceof` on `undefined`, failures in files you never touched) look like real regressions. Rebuild with `pnpm build` before believing them.

### Tailwind and package CSS

Tailwind v4 detects sources from the consuming app's own root and never descends into `node_modules`, where every `@forma/*` package is linked. Packages therefore ship their own CSS: `@forma/survey-ui` exports `./styles`, `@forma/surveys` inlines its styles into the bundle. Only two JS Tailwind configs remain (`packages/survey-ui/tailwind.config.ts`, `packages/surveys/tailwind.config.cjs`), both reached through an explicit `@config` from the package's stylesheet.

### Frontend conventions

- Routes are files under `apps/web/src/routes/`; `apps/web/src/routeTree.gen.ts` is generated by the router plugin during `vite build`/`vite dev` and committed. CI fails if it is stale.
- Server state goes through TanStack Query with `queryOptions` factories; route loaders prefetch through the same options. Do not keep a second copy of server data in component state. Calls go through `api` in `apps/web/src/lib/api.ts`, which throws `ApiError`.
- User-facing text goes through i18next (`useTranslation`, `t("<namespace>.<key>")`). The catalogues are `apps/web/locales/*.json`, carried over from forma2 with their keys; the `emails` namespace is rendered by the Go mailer, which embeds a copy in `apps/server/internal/platform/mailer/locales/` (after editing it, run `go test ./internal/platform/mailer -run TestCatalogueMatchesWebLocales -update` in `apps/server/`). `pnpm exec vp run scan-translations` (CI's translation check) fails on a key used in code but missing, a key nobody reads that `packages/i18n-utils/web-unused-keys.json` does not record (and on a recorded key that is used again), and an en-US value whose checksum in `apps/web/i18n.lock` is missing or stale: add or update the key's line there (`objectHash.MD5(value)`, as lingo.dev writes it) and remove a key you start reading from the unused list.
- Avoid `any` (`typescript/no-explicit-any` is an error), keep components focused, and meet basic accessibility.
- Lint and format rules live only in the root `vite.config.ts`; package configs carry build, test and dev settings. Per-path exceptions go in `lint.overrides` with a comment saying why.

## Testing

- Test behaviour and outcomes at the cheapest level that can fail on them.
- Go: table-driven tests next to the code; integration tests that need Postgres, Valkey or SpiceDB start real ones with testcontainers. `platform/pgtest` gives each test a fresh migrated database, `platform/valkey/valkeytest` a fresh Valkey database, and `mailer/mailertest.Transport` captures sent mail.
- TypeScript: Vitest next to the code.
- A ported module is done when forma2's tests for it are reproduced here and pass.

## GitHub Actions

- Always set minimal `permissions` for `GITHUB_TOKEN`.
- On `ubuntu-latest`, add `step-security/harden-runner` as the first step.
- Pin every third-party action to a full commit SHA with the version in a trailing comment.

## Commit & Pull Request Guidelines

Commits follow Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`, `test:`, `docs:`). Keep each PR to one module, use `.github/pull_request_template.md`, and record migrations or environment changes in it. Do not restate what CI already reports.
