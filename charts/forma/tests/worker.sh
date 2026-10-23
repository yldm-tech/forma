#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com)

render() {
  helm template worker-contract "${CHART_DIR}" "${COMMON_ARGS[@]}" "$@"
}

# One manifest (by kind and name) out of a full render.
manifest() {
  awk -v kind="$2" -v name="$3" '
    /^---/ { if (match_kind && match_name) print block; block = ""; match_kind = 0; match_name = 0; next }
    { block = block $0 "\n" }
    $0 == "kind: " kind { match_kind = 1 }
    $0 == "  name: " name { match_name = 1 }
    END { if (match_kind && match_name) print block }
  ' <<<"$1"
}

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

count_env() {
  grep --fixed-strings --line-regexp --count "            - name: $2" <<<"$1" || true
}

# --- Default: the server serves, the worker Deployment runs the jobs ---------------------------------------
default_render="$(render)"
server="$(manifest "${default_render}" Deployment forma)"
worker="$(manifest "${default_render}" Deployment forma-worker)"

[[ -n "${server}" ]] || fail "The server Deployment must render."
[[ -n "${worker}" ]] || fail "The worker Deployment must render by default."

# The server keeps the image's default command (`forma start`), steered by forma2's entrypoint switches.
if grep --quiet '^          args:' <<<"${server}"; then
  fail "The server must run the image's default command unless deployment.args says otherwise."
fi
grep --quiet --fixed-strings 'args: ["worker"]' <<<"${worker}" || fail "The worker must run forma worker."
[[ "$(count_env "${server}" SKIP_STARTUP_MIGRATION)" == "1" ]] || fail "The migration Job owns the schema, so the server must skip its startup migration."
[[ "$(count_env "${server}" BULLMQ_WORKER_ENABLED)" == "1" ]] || fail "With the worker Deployment, the server must not run the jobs too."
grep --quiet --fixed-strings --line-regexp '              value: "0"' <<<"${server}" || fail "BULLMQ_WORKER_ENABLED must be 0 on the server."
[[ "$(count_env "${worker}" BULLMQ_WORKER_ENABLED)" == "0" ]] || fail "The worker process ignores BULLMQ_WORKER_ENABLED; it must not be set there."

# The worker is selected apart from the server, so the Service, HPA and PDB keep addressing server pods only.
grep --quiet --fixed-strings 'app.kubernetes.io/component: worker' <<<"${worker}" || fail "Worker pods must carry the worker component label."
service="$(manifest "${default_render}" Service forma)"
if grep --quiet --fixed-strings 'app.kubernetes.io/component: worker' <<<"${service}"; then
  fail "The server Service must not select worker pods."
fi

# Same hardening and the same configuration as the server.
for line in "            runAsNonRoot: true" "            runAsUser: 1001" "            allowPrivilegeEscalation: false"; do
  grep --quiet --fixed-strings --line-regexp -- "${line}" <<<"${worker}" || fail "The worker container must render '${line## }'."
done
grep --quiet --fixed-strings 'name: forma-app-secrets' <<<"${worker}" || fail "The worker must read the app Secret."
for variable in AUTHZED_ENABLED AUTHZED_ENDPOINT AUTHZED_TOKEN AUTHZED_INSECURE TRUSTED_PROXY_HOP_COUNT; do
  [[ "$(count_env "${worker}" "${variable}")" == "1" ]] || fail "The worker must get ${variable} like the server."
done
# Booleans the Go server reads are 1 or 0; only SKIP_STARTUP_MIGRATION keeps forma2's "true".
spelled_out="$(awk '
  /^ *- name: / { name = $3 }
  /^ *value: "?(true|false)"?$/ && name != "SKIP_STARTUP_MIGRATION" { print name }
' <<<"${server}${worker}")"
[[ -z "${spelled_out}" ]] || fail "These variables must be 1 or 0, not true or false: ${spelled_out}"

# --- worker.enabled=false: the server runs the jobs in-process, as forma2's pods did ------------------------
inline_render="$(render --set worker.enabled=false)"
[[ -z "$(manifest "${inline_render}" Deployment forma-worker)" ]] || fail "worker.enabled=false must not render the worker Deployment."
[[ "$(count_env "$(manifest "${inline_render}" Deployment forma)" BULLMQ_WORKER_ENABLED)" == "0" ]] \
  || fail "Without the worker Deployment the server must keep its in-process worker."

# --- Operator overrides win, once -------------------------------------------------------------------------
override_server="$(manifest "$(render --set-string deployment.env.BULLMQ_WORKER_ENABLED=1)" Deployment forma)"
[[ "$(count_env "${override_server}" BULLMQ_WORKER_ENABLED)" == "1" ]] || fail "deployment.env.BULLMQ_WORKER_ENABLED must replace the chart default, not repeat it."

startup_migration_server="$(manifest "$(render --set migration.enabled=false)" Deployment forma)"
[[ "$(count_env "${startup_migration_server}" SKIP_STARTUP_MIGRATION)" == "0" ]] || fail "Without the migration Job the server must migrate at start."

# --- Metrics: both processes export, and the ServiceMonitor scrapes both -----------------------------------
scraped_render="$(render --api-versions monitoring.coreos.com/v1)"
[[ "$(count_env "$(manifest "${scraped_render}" Deployment forma-worker)" PROMETHEUS_ENABLED)" == "1" ]] || fail "The worker records the job metrics, so it must export them when scraped."
[[ -n "$(manifest "${scraped_render}" Service forma-worker-metrics)" ]] || fail "The worker needs a metrics Service for the ServiceMonitor."
monitor="$(manifest "${scraped_render}" ServiceMonitor forma-svc-monitor)"
grep --quiet --fixed-strings --line-regexp '          - worker' <<<"${monitor}" || fail "The ServiceMonitor must select the worker's metrics Service."
if [[ -n "$(manifest "${default_render}" Service forma-worker-metrics)" ]]; then
  fail "Without the Prometheus Operator nothing scrapes the worker, so its metrics Service must not render."
fi

printf '%s\n' "Forma worker contracts are valid."
