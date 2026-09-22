#!/usr/bin/env bash

# The bundled Valkey holds the BullMQ queue, and BullMQ job hashes are not cache entries: losing one loses
# the job. So the instance has to refuse writes at a ceiling it sets for itself rather than grow until the
# kernel OOM-kills it, and `maxmemory-policy noeviction` has to stay. That only works while `maxmemory` is
# set and stays well under the container limit — two numbers in two different files, which is exactly the
# kind of pair that drifts apart silently.

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly CHART_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
readonly COMMON_ARGS=(--set forma.webappUrl=https://qa.example.com)

# `appendonly yes` means an AOF rewrite forks the server, and copy-on-write during the rewrite can add most
# of the dataset again on top of the baseline RSS. Anything above this share of the limit makes the fork,
# not the dataset, the thing that gets the container killed.
readonly MAX_SHARE_OF_LIMIT_PERCENT=60

render() {
  local template="$1"
  shift

  helm template redis-memory "${CHART_DIR}" "${COMMON_ARGS[@]}" "$@" --show-only "templates/${template}"
}

# Kubernetes quantities: Mi/Gi are binary, M/G decimal.
kubernetes_quantity_to_bytes() {
  local quantity="$1" number="${1%%[A-Za-z]*}" suffix="${1##*[0-9]}"

  case "${suffix}" in
    Ki) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 }' ;;
    Mi) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 * 1024 }' ;;
    Gi) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 * 1024 * 1024 }' ;;
    K | k) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 }' ;;
    M) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 * 1000 }' ;;
    G) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 * 1000 * 1000 }' ;;
    "") printf '%d' "${number}" ;;
    *)
      printf '%s\n' "Unsupported memory quantity on the Valkey container: ${quantity}" >&2
      exit 1
      ;;
  esac
}

# redis.conf units: `mb` is binary (1mb = 1024*1024), bare `m` is decimal (1m = 1000*1000).
redis_size_to_bytes() {
  local size="$1" number="${1%%[a-z]*}" suffix="${1##*[0-9]}"

  case "${suffix}" in
    kb) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 }' ;;
    mb) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 * 1024 }' ;;
    gb) awk -v n="${number}" 'BEGIN { printf "%d", n * 1024 * 1024 * 1024 }' ;;
    k) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 }' ;;
    m) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 * 1000 }' ;;
    g) awk -v n="${number}" 'BEGIN { printf "%d", n * 1000 * 1000 * 1000 }' ;;
    "") printf '%d' "${number}" ;;
    *)
      printf '%s\n' "Unsupported maxmemory unit in the rendered valkey.conf: ${size}" >&2
      exit 1
      ;;
  esac
}

configuration="$(render redis-configmap.yaml)"
statefulset="$(render redis-statefulset.yaml)"

# `maxmemory-policy` also starts with `maxmemory`, so anchor on the directive taking a size.
maxmemory="$(sed -n 's/^[[:space:]]*maxmemory[[:space:]][[:space:]]*\([0-9][0-9a-z]*\)[[:space:]]*$/\1/p' <<<"${configuration}")"
if [[ -z "${maxmemory}" ]]; then
  printf '%s\n' "The rendered valkey.conf must set an explicit maxmemory; without one Valkey grows until the kernel kills it." >&2
  printf '%s\n' "${configuration}" >&2
  exit 1
fi

policy="$(sed -n 's/^[[:space:]]*maxmemory-policy[[:space:]][[:space:]]*\([a-z][a-z-]*\)[[:space:]]*$/\1/p' <<<"${configuration}")"
if [[ "${policy}" != "noeviction" ]]; then
  printf '%s\n' "maxmemory-policy must stay noeviction: an eviction policy silently drops BullMQ job hashes. Got: ${policy:-<unset>}" >&2
  exit 1
fi

# The Valkey container is the only one in this StatefulSet, so the first `limits:` block is its own. Matched
# by relative indentation rather than a fixed column, so re-nesting the template does not silently skip it.
memory_limit="$(awk '
  /^[[:space:]]*limits:[[:space:]]*$/ {
    match($0, /[^ ]/)
    limit_indent = RSTART
    in_limits = 1
    next
  }
  in_limits {
    match($0, /[^ ]/)
    if (RSTART <= limit_indent) { in_limits = 0; next }
    if ($1 == "memory:") { print $2; exit }
  }
' <<<"${statefulset}")"
if [[ -z "${memory_limit}" ]]; then
  printf '%s\n' "The Valkey container must declare a memory limit for maxmemory to be measured against." >&2
  exit 1
fi

maxmemory_bytes="$(redis_size_to_bytes "${maxmemory}")"
limit_bytes="$(kubernetes_quantity_to_bytes "${memory_limit}")"
share="$(awk -v m="${maxmemory_bytes}" -v l="${limit_bytes}" 'BEGIN { printf "%d", (m * 100) / l }')"

if ((share > MAX_SHARE_OF_LIMIT_PERCENT)); then
  printf '%s\n' \
    "maxmemory ${maxmemory} is ${share}% of the ${memory_limit} container limit, above the ${MAX_SHARE_OF_LIMIT_PERCENT}% ceiling." \
    "Raise redis.master.resources.limits.memory or lower redis.commonConfiguration's maxmemory so an AOF rewrite fork still fits." >&2
  exit 1
fi

printf '%s\n' "Valkey maxmemory ${maxmemory} is ${share}% of the ${memory_limit} container limit, with policy ${policy}."
