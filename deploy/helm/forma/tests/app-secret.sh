#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com --set deployment.image.tag=0.2.0)

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

# Without cluster access, lookup finds no existing Secret, so its values are fresh.
render() {
  helm template app-secret "${CHART_DIR}" "${COMMON_ARGS[@]}" "$@"
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

has_key() {
  grep --quiet --extended-regexp "^  $2: [A-Za-z0-9+/=]+$" <<<"$1"
}

count_env() {
  grep --fixed-strings --line-regexp --count "            - name: $2" <<<"$1" || true
}

# --- Default: the generated Secret --------------------------------------------------------------------------
secret="$(render --show-only templates/secrets.yaml)"
for key in WEBAPP_URL ENCRYPTION_KEY NEXTAUTH_SECRET; do
  has_key "${secret}" "${key}" || fail "The app Secret must hold ${key}."
done
# NEXTAUTH_URL equalled WEBAPP_URL, which the server falls back to; the other former names were never written.
for key in NEXTAUTH_URL BETTER_AUTH_SECRET BETTER_AUTH_URL; do
  if grep --quiet "^  ${key}:" <<<"${secret}"; then
    fail "The app Secret must not hold ${key}, a former name the server reads only for compatibility."
  fi
done
# The link survey token secret keeps its former name while the server still reads it: Kubernetes lets an operator's NEXTAUTH_SECRET (deployment.env, or a deployment.envFrom source after the chart's) replace the chart's only under the same name, and the server would read a chart-generated LINK_SURVEY_TOKEN_SECRET before it.
if grep --quiet "^  LINK_SURVEY_TOKEN_SECRET:" <<<"${secret}"; then
  fail "The app Secret must keep the link survey token secret under NEXTAUTH_SECRET, or it would take precedence over an operator's NEXTAUTH_SECRET."
fi

# --- An operator's NEXTAUTH_SECRET in deployment.env stays in force -------------------------------------------
legacy_render="$(render --set-string deployment.env.NEXTAUTH_SECRET=operator-supplied-link-survey-token-secret)"
if grep --quiet --extended-regexp '^ *(- name: )?LINK_SURVEY_TOKEN_SECRET:?$|^ *LINK_SURVEY_TOKEN_SECRET: ' <<<"${legacy_render}"; then
  fail "With deployment.env.NEXTAUTH_SECRET, nothing the chart renders may set LINK_SURVEY_TOKEN_SECRET, which the server reads first."
fi
for deployment in forma forma-worker; do
  [[ "$(count_env "$(manifest "${legacy_render}" Deployment "${deployment}")" NEXTAUTH_SECRET)" == "1" ]] \
    || fail "deployment.env.NEXTAUTH_SECRET must reach the ${deployment} container, where it replaces the Secret's value."
done

# --- An operator's LINK_SURVEY_TOKEN_SECRET in deployment.env replaces the generated one --------------------
current_render="$(render --set-string deployment.env.LINK_SURVEY_TOKEN_SECRET=operator-supplied-link-survey-token-secret)"
current_secret="$(manifest "${current_render}" Secret forma-app-secrets)"
[[ -n "${current_secret}" ]] || fail "The app Secret must render."
if grep --quiet "^  NEXTAUTH_SECRET:" <<<"${current_secret}"; then
  fail "With deployment.env.LINK_SURVEY_TOKEN_SECRET, the app Secret must leave NEXTAUTH_SECRET out: the server would ignore it and warn about it."
fi
[[ "$(count_env "$(manifest "${current_render}" Deployment forma)" LINK_SURVEY_TOKEN_SECRET)" == "1" ]] \
  || fail "deployment.env.LINK_SURVEY_TOKEN_SECRET must be rendered once."

printf '%s\n' "Forma app Secret contracts are valid."
