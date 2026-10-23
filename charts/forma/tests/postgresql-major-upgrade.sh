#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com --set deployment.image.tag=0.2.0 --set authzed.migrationAcknowledged=true)
# The PostgreSQL subchart refuses to render an upgrade without the current passwords, which it would otherwise look up.
readonly UPGRADE=(--is-upgrade --set postgresql.auth.password=placeholder --set postgresql.auth.postgresPassword=placeholder)

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

# helm template has no cluster, so lookup finds nothing. A copy of the chart stands in for one whose bundled PostgreSQL StatefulSet runs the image given: the lookup of the running image is replaced by that image.
chart_running() {
  local image="$1" copy="${work_dir}/$2"
  rm -rf "${copy}"
  cp -R "${CHART_DIR}" "${copy}"
  grep -q 'lookup "apps/v1" "StatefulSet"' "${copy}/templates/_helpers.tpl" || fail "The running PostgreSQL image is no longer read through lookup; update this test."
  sed -i.bak "s#lookup \"apps/v1\" \"StatefulSet\" .Release.Namespace \$name#dict \"spec\" (dict \"template\" (dict \"spec\" (dict \"containers\" (list (dict \"name\" \"postgresql\" \"image\" \"${image}\")))))#" "${copy}/templates/_helpers.tpl"
  printf '%s' "${copy}"
}

# A fresh install, and an upgrade where nothing can be looked up (helm template, Argo CD), render.
helm template qa "${CHART_DIR}" "${COMMON_ARGS[@]}" >/dev/null
helm template qa "${CHART_DIR}" "${COMMON_ARGS[@]}" "${UPGRADE[@]}" >/dev/null

# Upgrading a release whose bundled database still runs forma2's PostgreSQL 17 stops before anything is applied.
pg17="$(chart_running docker.io/pgvector/pgvector:pg17 pg17)"
if output="$(helm template qa "${pg17}" "${COMMON_ARGS[@]}" "${UPGRADE[@]}" 2>&1)"; then
  fail "An upgrade over a PostgreSQL 17 data directory rendered without postgresql.majorUpgradeAcknowledged."
fi
grep -q 'runs PostgreSQL 17' <<<"${output}" || fail "The refusal does not name the running major version: ${output}"
grep -q 'postgresql.majorUpgradeAcknowledged=true' <<<"${output}" || fail "The refusal does not say how to proceed: ${output}"

# After the dump and restore the operator acknowledges it, and the upgrade renders.
helm template qa "${pg17}" "${COMMON_ARGS[@]}" "${UPGRADE[@]}" --set postgresql.majorUpgradeAcknowledged=true >/dev/null

# A release already on PostgreSQL 18, or one using an external database, upgrades as usual.
pg18="$(chart_running docker.io/pgvector/pgvector:pg18 pg18)"
helm template qa "${pg18}" "${COMMON_ARGS[@]}" "${UPGRADE[@]}" >/dev/null
helm template qa "${pg17}" "${COMMON_ARGS[@]}" "${UPGRADE[@]}" --set postgresql.enabled=false --set postgresql.externalDatabaseUrl=postgresql://forma:secret@db.example:5432/forma --set authzed.datastore.existingSecret=spicedb-datastore >/dev/null

# The procedure the refusal points to is documented.
grep -q '^## Upgrading the bundled PostgreSQL from 17 to 18' "${CHART_DIR}/README.md" || fail "README.md lacks the PostgreSQL upgrade procedure."

echo "postgresql major upgrade guard: ok"
