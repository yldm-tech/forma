#!/usr/bin/env bash
# Starts the running instance the browser suite (apps/e2e/, .github/workflows/e2e.yml) drives: docker-compose.dev.yml's PostgreSQL, Valkey, SpiceDB and RustFS, then bin/forma (built beforehand, `make build` or `make server`) migrated, with the SpiceDB schema written, optionally seeded, and serving with its job worker as `forma start` does in the container.
#
#   tooling/scripts/ci-stack.sh up      start everything; SEED_ARGS="" seeds the base data, and any flags in it are passed to `forma seed`
#   tooling/scripts/ci-stack.sh restart stop the server and start it again with the environment as it is now (a second configuration, such as TURNSTILE_* for the Turnstile spec), seeding first when SEED_ARGS is set; the containers and their data stay
#   tooling/scripts/ci-stack.sh down    stop the server and the containers
#
# Configuration is .env.example's, under the overrides below; any variable already set wins over both, so a local run can move the ports (POSTGRES_PORT, VALKEY_PORT, SPICEDB_GRPC_PORT, RUSTFS_PORT, with DATABASE_URL, REDIS_URL, AUTHZED_ENDPOINT and S3_ENDPOINT_URL to match) or the compose project (COMPOSE_PROJECT_NAME). The server's output goes to FORMA_LOG (default forma.log in a temporary directory, printed at start).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

stop_server() {
  if [[ -f "${pid_file}" ]]; then
    local pid
    pid="$(cat "${pid_file}")"
    kill "${pid}" 2>/dev/null || true
    # Waits for the port to be free before a restart binds it again.
    for _ in $(seq 1 30); do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 1
    done
    if kill -0 "${pid}" 2>/dev/null; then
      echo "forma (pid ${pid}) did not stop within 30 s." >&2
      return 1
    fi
    rm -f "${pid_file}"
  fi
}

down() {
  # The containers go even when the server would not stop.
  stop_server || true
  "${compose[@]}" down --volumes
}

seed() {
  if [[ -n "${SEED_ARGS+set}" ]]; then
    # shellcheck disable=SC2086 # SEED_ARGS is a list of flags
    ALLOW_SEED=1 "${forma}" seed ${SEED_ARGS}
    # Deliver the seeded tenancy to SpiceDB now rather than on the worker's next tick, so the first request already sees it.
    "${forma}" authz outbox drain
  fi
}

start_server() {
  local ready="http://localhost:${HTTP_ADDR##*:}/health/ready"
  # A server still answering here (one this script lost track of) would pass the readiness check below for the new one.
  if curl -fs --max-time 5 -o /dev/null "${ready}"; then
    echo "Something already serves ${ready}; stop it first." >&2
    exit 1
  fi
  echo "forma start logs to ${log_file}"
  "${forma}" start >>"${log_file}" 2>&1 &
  echo $! >"${pid_file}"

  for _ in $(seq 1 120); do
    if ! kill -0 "$(cat "${pid_file}")" 2>/dev/null; then
      echo "forma start exited; its log:" >&2
      cat "${log_file}" >&2
      exit 1
    fi
    if curl -fs --max-time 5 -o /dev/null "${ready}"; then
      echo "forma is ready at ${WEBAPP_URL}"
      return 0
    fi
    sleep 1
  done
  echo "forma did not become ready at ${ready}; its log:" >&2
  cat "${log_file}" >&2
  exit 1
}

restart() {
  stop_server
  seed
  start_server
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

  seed
  : >"${log_file}"
  start_server
}

case "${1:-up}" in
  up) up ;;
  restart) restart ;;
  down) down ;;
  *)
    echo "usage: $0 [up|restart|down]" >&2
    exit 2
    ;;
esac
