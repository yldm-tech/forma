#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com --set deployment.image.tag=0.2.0)

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

# The restricted Pod Security Standard rejects a container missing any one of these. runAsUser pins the uid the image is built for (USER 1001:1001), so an image built elsewhere cannot silently run as another user.
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

# readOnlyRootFilesystem would break large multipart uploads, which spill to /tmp. Enabling it is an opt-in that has to come with an emptyDir at /tmp.
if grep --fixed-strings --line-regexp "            readOnlyRootFilesystem: true" <<<"${default_context}" >/dev/null; then
  printf '%s\n' "readOnlyRootFilesystem must stay off by default; large uploads spill to /tmp." >&2
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

# --- Liveness and readiness must not be the same signal -------------------------------------------------
#
# /health is a static handler: it can only fail when the process is gone, which is exactly what liveness
# wants and exactly what makes it useless as readiness. Aliasing the two lets a rollout whose pods cannot
# reach Postgres go Ready and replace every serving pod.
probe_block() {
  sed -n "/^          $2:\$/,/^          [a-zA-Z]/p" <<<"$1" | sed '$d'
}

readiness_probe="$(probe_block "${default_deployment}" readinessProbe)"
liveness_probe="$(probe_block "${default_deployment}" livenessProbe)"

assert_contains "${readiness_probe}" "              path: /health/ready" \
  "Readiness must probe the dependency-aware /health/ready route, not the static /health stub."
assert_contains "${liveness_probe}" "              path: /health" \
  "Liveness must stay on /health so a dependency outage cannot restart every pod."
if grep --fixed-strings --line-regexp "              path: /health/ready" <<<"${liveness_probe}" >/dev/null; then
  printf '%s\n' "Liveness must not probe the readiness route: a Postgres outage would become a crash loop." >&2
  exit 1
fi

# --- The metrics exporter binds exactly where something scrapes it --------------------------------------
#
# The server only starts the Prometheus exporter when PROMETHEUS_ENABLED is set, so a ServiceMonitor rendered without it scrapes a closed :9464 and the install exports nothing.
prometheus_env_count() {
  grep --fixed-strings --line-regexp --count "            - name: PROMETHEUS_ENABLED" <<<"$1" || true
}

if [[ "$(prometheus_env_count "${default_deployment}")" != "0" ]]; then
  printf '%s\n' "Without the Prometheus Operator CRD nothing scrapes the app, so PROMETHEUS_ENABLED must stay unset." >&2
  exit 1
fi

scraped_deployment="$(render_deployment hardening-scraped --api-versions monitoring.coreos.com/v1)"
assert_contains "${scraped_deployment}" "            - name: PROMETHEUS_ENABLED" \
  "A rendered ServiceMonitor must come with the PROMETHEUS_ENABLED that makes :9464 listen."
assert_contains "${scraped_deployment}" '              value: "1"' \
  "PROMETHEUS_ENABLED must be the literal \"1\" the exporter checks for."

unscraped_deployment="$(render_deployment hardening-unscraped \
  --api-versions monitoring.coreos.com/v1 --set serviceMonitor.enabled=false)"
if [[ "$(prometheus_env_count "${unscraped_deployment}")" != "0" ]]; then
  printf '%s\n' "With serviceMonitor disabled nothing scrapes the app, so PROMETHEUS_ENABLED must stay unset." >&2
  exit 1
fi

# An operator who sets the variable themselves owns it, in either direction.
opted_out_deployment="$(render_deployment hardening-opted-out \
  --api-versions monitoring.coreos.com/v1 --set deployment.env.PROMETHEUS_ENABLED=0)"
if [[ "$(prometheus_env_count "${opted_out_deployment}")" != "1" ]]; then
  printf '%s\n' "deployment.env must override the chart default rather than render PROMETHEUS_ENABLED twice." >&2
  exit 1
fi
assert_contains "${opted_out_deployment}" '              value: "0"' \
  "An explicit deployment.env.PROMETHEUS_ENABLED must win over the ServiceMonitor default."

# The image has no usable default: the release workflow publishes 0.2.<run>, sha-<commit> and latest, never the chart's appVersion, so a render without a tag or digest fails instead of deploying an image that does not exist.
if untagged="$(helm template hardening-untagged "${CHART_DIR}" --set forma.webappUrl=https://qa.example.com --show-only templates/deployment.yaml 2>&1)"; then
  printf '%s\n' "A render without deployment.image.tag or deployment.image.digest must fail." "${untagged}" >&2
  exit 1
fi
if ! grep --quiet 'deployment.image.tag' <<<"${untagged}"; then
  printf '%s\n' "The refusal must name deployment.image.tag." "${untagged}" >&2
  exit 1
fi
assert_contains "$(render_deployment hardening-tagged)" '          image: ghcr.io/yldm-tech/forma-server:0.2.0' \
  "deployment.image.tag must name the image."
assert_contains "$(render_deployment hardening-digest --set deployment.image.digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef)" \
  '          image: ghcr.io/yldm-tech/forma-server@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' \
  "deployment.image.digest must take precedence over the tag."

printf '%s\n' "Forma app container hardening contracts are valid."
