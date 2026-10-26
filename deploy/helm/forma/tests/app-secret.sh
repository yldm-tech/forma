#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

# The generated app Secret, without cluster access: lookup finds no existing Secret, so its values are fresh.
secret="$(helm template app-secret "${CHART_DIR}" --set forma.webappUrl=https://qa.example.com --set deployment.image.tag=0.2.0 --show-only templates/secrets.yaml)"

# The variables carry their current names; the server still reads forma2's names, but the chart no longer writes them.
for key in WEBAPP_URL ENCRYPTION_KEY LINK_SURVEY_TOKEN_SECRET; do
  grep --quiet --extended-regexp "^  ${key}: [A-Za-z0-9+/=]+$" <<<"${secret}" || fail "The app Secret must hold ${key}."
done
for key in NEXTAUTH_URL NEXTAUTH_SECRET BETTER_AUTH_SECRET BETTER_AUTH_URL; do
  if grep --quiet "^  ${key}:" <<<"${secret}"; then
    fail "The app Secret must not hold ${key}, a former name the server reads only for compatibility."
  fi
done

printf '%s\n' "Forma app Secret contracts are valid."
