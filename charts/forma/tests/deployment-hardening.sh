#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com)

render_deployment() {
  local release_name="$1"
  shift

  helm template "${release_name}" "${CHART_DIR}" "${COMMON_ARGS[@]}" "$@" \
    --show-only templates/deployment.yaml
}

# The app container securityContext is indented by ten spaces; the pod-level one by six. Anchoring on the
# indentation is what keeps these assertions from passing on the wrong block.
container_security_context() {
  sed -n '/^          securityContext:$/,/^          [a-zA-Z]/p' <<<"$1" | sed '$d'
}

assert_contains() {
  local haystack="$1" needle="$2" message="$3"

  if ! grep --fixed-strings --line-regexp "${needle}" <<<"${haystack}" >/dev/null; then
    printf '%s\n' "${message}" >&2
    printf '%s\n' "${haystack}" >&2
    exit 1
  fi
}

default_deployment="$(render_deployment hardening-default)"
default_context="$(container_security_context "${default_deployment}")"

if [[ -z "${default_context}" ]]; then
  printf '%s\n' "The default install must render a container securityContext on the app container." >&2
  exit 1
fi

# The restricted Pod Security Standard rejects a container missing any one of these, and kubelet cannot
# verify runAsNonRoot without a numeric runAsUser because the image declares the non-numeric `USER nextjs`.
assert_contains "${default_context}" "            allowPrivilegeEscalation: false" \
  "The app container must disable privilege escalation by default."
assert_contains "${default_context}" "              - ALL" \
  "The app container must drop all capabilities by default."
assert_contains "${default_context}" "            runAsNonRoot: true" \
  "The app container must run as a non-root user by default."
assert_contains "${default_context}" "            runAsUser: 1001" \
  "The app container must pin the numeric uid the image is built for."
assert_contains "${default_context}" "              type: RuntimeDefault" \
  "The app container must request the RuntimeDefault seccomp profile by default."

# readOnlyRootFilesystem would break file uploads and the Next.js standalone cache, both of which live on
# the container root filesystem. Enabling it is an opt-in that has to come with emptyDir mounts.
if grep --fixed-strings --line-regexp "            readOnlyRootFilesystem: true" <<<"${default_context}" >/dev/null; then
  printf '%s\n' "readOnlyRootFilesystem must stay off by default; the app writes to uploads and the Next.js cache." >&2
  exit 1
fi

# The block is operator-controlled rather than hardcoded, in both directions.
overridden_deployment="$(render_deployment hardening-override \
  --set-json 'deployment.containerSecurityContext={"runAsUser":2000}')"
overridden_context="$(container_security_context "${overridden_deployment}")"
assert_contains "${overridden_context}" "            runAsUser: 2000" \
  "An operator-supplied containerSecurityContext must reach the app container."

# Helm coalesces maps, so `{}` keeps the chart defaults and only an explicit null opts out entirely.
cleared_deployment="$(render_deployment hardening-cleared \
  --set-json 'deployment.containerSecurityContext=null')"
if [[ -n "$(container_security_context "${cleared_deployment}")" ]]; then
  printf '%s\n' "A null containerSecurityContext must render no securityContext block at all." >&2
  exit 1
fi

printf '%s\n' "Forma app container hardening contracts are valid."
