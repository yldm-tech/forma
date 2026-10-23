# v3 API contract tests

Two checks keep the v3 management API and its contract, [`../openapi.yml`](../openapi.yml) (the committed bundle), in step.

- **Route parity**, `TestV3RouteParity` in [`server/internal/app/v3_routes_test.go`](../../../server/internal/app/v3_routes_test.go): builds the production router and fails when it serves an `/api/v3` operation the bundle does not document, or the bundle documents one it does not serve. It is a Go test, so it runs with the rest of the Go suite in `pr.yml`. Its two exception lists, `documentedWithoutRoute` and `routeWithoutDocumentation`, may only shrink: an entry that has stopped being true fails the test too.
- **Schemathesis**, [`.github/workflows/api-v3-contract-tests.yml`](../../../.github/workflows/api-v3-contract-tests.yml): drives a running, seeded server against the bundle and fails when a response violates the documented status codes, content type or schema, or when anything answers 5xx. `pr.yml` calls it, so it is part of the `PR Check Summary` required check.

`api-v3-spec.yml` already lints the source tree and fails on a stale bundle. That proves the document is well-formed and current; these prove the running API matches it.

## What is in here

| File                | Purpose                                                                                                                                                                   |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `hooks.py`          | Registers the `cuid2` format and substitutes seeded identifiers, keyed by parameter name and operationId.                                                                 |
| `schemathesis.toml` | Request timeout and `fill-missing`, which gives every documented operation a case even when the spec carries no example for it.                                           |
| `fixtures.json`     | Written by `forma seed --contract`, gitignored. The id map `hooks.py` reads: the seeded workspace, the read fixtures, and one disposable resource per mutating operation. |

Coverage is automatic: a newly documented operation is exercised as soon as it is in the bundle. If its identifiers are not in `fixtures.json` it runs against a generated id and gets the documented 403, still a real contract check, just a shallower one. To exercise it against real data, add the resource in `server/internal/seed/contract.go`.

## How the server is prepared

`forma seed` (`server/internal/seed`, forma2's `db:seed`) writes the demo organization, its users, a workspace with forma2's demo surveys and responses, and demo workflows with run history. `ALLOW_SEED=1` is required. With `SEED_API_KEY` set it also creates an API key, sent as `fbk_<SEED_API_KEY>`, with manage access to the seeded workspace and no organization-level grants. `--contract` (forma2's `db:seed:contract`) adds the disposable surveys, workflows and action class the suite mutates and writes the id map to `--out` (default `docs/api-v3-reference/contract-tests/fixtures.json`, relative to the working directory). Every seeded row has a fixed id, so seeding again rewrites the same rows.

[`scripts/ci-stack.sh`](../../../scripts/ci-stack.sh) puts it together, in CI and locally: it starts PostgreSQL, Valkey, SpiceDB and RustFS from `docker-compose.dev.yml`, migrates, writes the SpiceDB schema, seeds with `SEED_ARGS`, delivers the seeded authorization data to SpiceDB, and starts `bin/forma start` on `:3000` with rate limits off.

## Running it locally

```bash
# 1. Build the server and start a seeded instance. Any throwaway secret works; nothing is committed.
make server
export SEED_API_KEY="$(openssl rand -hex 32)"
SEED_ARGS=--contract bash scripts/ci-stack.sh up

# 2. Run the suite (uv keeps the pinned version out of your global site-packages).
SCHEMATHESIS_HOOKS=docs/api-v3-reference/contract-tests/hooks.py \
  uvx --from 'schemathesis==4.25.0' schemathesis \
  --config-file docs/api-v3-reference/contract-tests/schemathesis.toml \
  run docs/api-v3-reference/openapi.yml \
  --url http://localhost:3000 \
  --phases examples \
  --checks not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance \
  --exclude-path-regex '^/api/v3/responses' \
  -H "x-api-key: fbk_${SEED_API_KEY}"

# 3. Stop the server and remove the containers and their volumes.
bash scripts/ci-stack.sh down
```

`scripts/ci-stack.sh` uses `docker-compose.dev.yml`'s ports; if your own development stack holds them, stop it first or move this one with the variables its header lists. The route parity test needs nothing running: `cd server && go test ./internal/app -run TestV3RouteParity`.

Things to know before reading the result:

- **Re-seed between runs.** The mutating operations consume their fixtures: the survey `DELETE` deletes one, `enable` flips another out of `draft`. `ALLOW_SEED=1 SEED_API_KEY=... bin/forma seed --contract` puts them back. CI always starts from an empty database, so it only matters locally: a second run without re-seeding turns those operations into documented 403s and quietly loses depth.
- **The four `/tags` operations are session-only** (the spec declares only `sessionAuth`), so an API key gets a documented 401 before any handler runs. They are checked against that 401, which is a real assertion that they stay session-only, and no tag fixtures exist, because nothing an API key sends can reach them. They carry `x-excluded` (no docs page) and are still tested.
- **The eight `/api/v3/responses` operations are documented but not routed.** forma2 agreed that contract before building it (ENG-2868) and never routed it, so the job passes `--exclude-path-regex '^/api/v3/responses'` and the route parity test lists them in `documentedWithoutRoute`. The bundle documents 36 operations and this job exercises 28. Remove the flag and the entries together when the routes land.
- **`POST /api/v3/surveys/generate`** answers its documented 403 (`ai_smart_tools_disabled`): the seeded organization has AI smart tools off and the CI instance has no AI provider.
- **Rate limiting** is off (`RATE_LIMITING_DISABLED=1`), otherwise a burst of cases turns into documented but uninteresting 429s.

For deeper local exploration, drop `--phases examples` for `--phases coverage` (boundary values) or `--phases fuzzing -n 50`. Both are far slower than the PR budget allows, which is why CI runs examples only.
