#!/usr/bin/env bash
set -euo pipefail

# Writes authzed/schema.zed into the local SpiceDB started by docker-compose.dev.yml.
#
# Starting the container is not enough: docker compose only runs SpiceDB's datastore migration,
# which creates its tables and leaves it with no schema. Until the schema is written every
# permission check fails with FAILED_PRECONDITION, which the app surfaces as "Error loading
# resources" on any page that reads a resource — a symptom that points nowhere near the cause.
#
# Two things make this awkward to run straight from `db:up`, and both are handled below.
#
# The CLI is TypeScript that imports @forma/* packages through their built `dist/`, but `db:up`
# runs before anything is built (`pnpm install && pnpm db:up && pnpm dev`). On a fresh clone the
# import fails, and the CLI reports that as `authzed_internal` — the same code a genuine SpiceDB
# failure produces, so the output cannot tell you which one you hit. The build below is what
# `pnpm dev` would do anyway and is a turbo cache hit (~0.2s) once warm.
#
# The write also happens moments after the container starts. SpiceDB's gRPC health probe reports
# healthy as soon as it accepts connections, which is not the same as being ready to accept a
# schema write, so this retries the write itself rather than trusting a proxy for it. Applying an
# already-current schema reports "unchanged" and exits 0, so retrying — and re-running this script
# against a provisioned stack — is always safe.

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly MAX_ATTEMPTS="${FORMA_AUTHZED_SCHEMA_ATTEMPTS:-10}"
readonly RETRY_DELAY_SECONDS="${FORMA_AUTHZED_SCHEMA_RETRY_DELAY:-3}"

cd -- "${REPO_ROOT}"

if ! pnpm build --filter=@forma/web^... >/dev/null 2>&1; then
  printf 'Could not build the packages the AuthZed schema CLI imports. Run `pnpm build --filter=@forma/web^...` to see why.\n' >&2
  exit 1
fi

last_output=""

for (( attempt = 1; attempt <= MAX_ATTEMPTS; attempt++ )); do
  if last_output="$(pnpm --silent authzed:schema apply 2>&1)"; then
    printf '%s\n' "${last_output}"
    exit 0
  fi

  if (( attempt < MAX_ATTEMPTS )); then
    printf 'SpiceDB is not ready for a schema write yet (attempt %d/%d); retrying in %ds.\n' \
      "${attempt}" "${MAX_ATTEMPTS}" "${RETRY_DELAY_SECONDS}" >&2
    sleep "${RETRY_DELAY_SECONDS}"
  fi
done

printf 'Failed to write the SpiceDB schema after %d attempts. Last output:\n%s\n' \
  "${MAX_ATTEMPTS}" "${last_output}" >&2
printf 'The stack is up but unauthorized: every permission check fails until this succeeds. Retry with `pnpm authzed:schema apply`.\n' >&2
exit 1
