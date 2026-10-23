#!/usr/bin/env bash
# Starts the running instance the browser suite (e2e/, .github/workflows/e2e.yml) and the v3 contract tests (.github/workflows/api-v3-contract-tests.yml) drive: docker-compose.dev.yml's PostgreSQL, Valkey, SpiceDB and RustFS, then bin/forma (built beforehand, `make build` or `make server`) migrated, with the SpiceDB schema written, optionally seeded, and serving with its job worker as `forma start` does in the container.
#
#   scripts/ci-stack.sh up      start everything; SEED_ARGS="" seeds the base data, SEED_ARGS="--contract" also the contract fixtures
#   scripts/ci-stack.sh down    stop the server and the containers
#
# Configuration is .env.example's, under the overrides below; any variable already set wins over both, so a local run can move the ports (POSTGRES_PORT, VALKEY_PORT, SPICEDB_GRPC_PORT, RUSTFS_PORT, with DATABASE_URL, REDIS_URL, AUTHZED_ENDPOINT and S3_ENDPOINT_URL to match) or the compose project (COMPOSE_PROJECT_NAME). The server's output goes to FORMA_LOG (default forma.log in a temporary directory, printed at start).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
compose=(docker compose -f docker-compose.dev.yml)
forma="${FORMA_BIN:-${repo_root}/bin/forma}"
# The server's pid and log live outside the checkout, one directory per checkout.
state_dir="${TMPDIR:-/tmp}/forma-ci-stack-$(printf '%s' "${repo_root}" | cksum | cut -d' ' -f1)"
mkdir -p "${state_dir}"
log_file="${FORMA_LOG:-${state_dir}/forma.log}"
pid_file="${state_dir}/forma.pid"

# The browser talks to the Go server directly (no Vite dev server), sign-up is open as forma2's E2E_TESTING made it, a new account can sign in without opening a verification email, nothing calls out to Have I Been Pwned, and a burst of test requests is never answered with 429.
export WEBAPP_URL="${WEBAPP_URL:-http://localhost:3000}"
export E2E_TESTING="${E2E_TESTING:-1}"
export EMAIL_VERIFICATION_DISABLED="${EMAIL_VERIFICATION_DISABLED:-1}"
export PASSWORD_HIBP_CHECK_DISABLED="${PASSWORD_HIBP_CHECK_DISABLED:-1}"
export RATE_LIMITING_DISABLED="${RATE_LIMITING_DISABLED:-1}"
export LOG_FORMAT="${LOG_FORMAT:-text}"
while IFS='=' read -r key value; do
  [[ "${key}" =~ ^[A-Z][A-Z0-9_]*$ ]] || continue
  if [[ -z "${!key+set}" ]]; then
    export "${key}=${value}"
  fi
done <.env.example

down() {
  if [[ -f "${pid_file}" ]]; then
    kill "$(cat "${pid_file}")" 2>/dev/null || true
    rm -f "${pid_file}"
  fi
  "${compose[@]}" down --volumes
}

up() {
  if [[ ! -x "${forma}" ]]; then
    echo "${forma} is missing: build it first (make build, or make server for the API alone)." >&2
    exit 1
  fi

  "${compose[@]}" up -d --wait postgres valkey spicedb rustfs
  "${compose[@]}" run --rm rustfs-init

  "${forma}" migrate
  # SpiceDB reports healthy a moment before it accepts a schema write (see the Makefile's authz-schema).
  for attempt in $(seq 1 10); do
    if "${forma}" authz schema apply; then
      break
    fi
    if ((attempt == 10)); then
      echo "SpiceDB never accepted the schema." >&2
      exit 1
    fi
    sleep 3
  done

  if [[ -n "${SEED_ARGS+set}" ]]; then
    # shellcheck disable=SC2086 # SEED_ARGS is a list of flags
    ALLOW_SEED=1 "${forma}" seed ${SEED_ARGS}
    # Deliver the seeded tenancy to SpiceDB now rather than on the worker's next tick, so the first request already sees it.
    "${forma}" authz outbox drain
  fi

  echo "forma start logs to ${log_file}"
  "${forma}" start >"${log_file}" 2>&1 &
  echo $! >"${pid_file}"

  local ready="http://localhost:${HTTP_ADDR##*:}/health/ready"
  for _ in $(seq 1 120); do
    if ! kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "forma start exited; its log:" >&2
      cat "${log_file}" >&2
      exit 1
    fi
    if curl -fs -o /dev/null "${ready}"; then
      echo "forma is ready at ${WEBAPP_URL}"
      return 0
    fi
    sleep 1
  done
  echo "forma did not become ready at ${ready}; its log:" >&2
  cat "${log_file}" >&2
  exit 1
}

case "${1:-up}" in
  up) up ;;
  down) down ;;
  *)
    echo "usage: $0 [up|down]" >&2
    exit 2
    ;;
esac
