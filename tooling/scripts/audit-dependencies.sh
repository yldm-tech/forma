#!/usr/bin/env bash
# The dependency vulnerability gate (.github/workflows/dependency-audit.yml). One scan per call:
#
#   audit-dependencies.sh go    govulncheck over apps/server. Fails on a vulnerability the server's code reaches (a symbol-level finding) unless apps/server/govulncheck-accepted.txt lists it; a vulnerable module or package the code never calls into is reported and passes.
#   audit-dependencies.sh npm   pnpm audit of the workspace's production dependencies (dependencies and optionalDependencies, not devDependencies, which only build and test). Fails on a high or critical advisory.
#
# The vulnerability data comes over the network, and an outage of vuln.go.dev, the Go module proxy or the npm registry says nothing about the change under test, so it must not fail a pull request: every fetch is retried, the Go database falls back to the copy in GOVULNDB_DIR (which CI restores from its cache), and a scan that still has no data passes with a warning annotation. A scan that has its data and finds something, or a server that does not load, fails.
#
# Settings (environment): GOVULNCHECK_VERSION (the golang.org/x/vuln release), GOVULNDB_URL, GOVULNDB_DIR (where the database is unpacked and kept), AUDIT_ATTEMPTS (tries per fetch, 10 s then 20 s ... apart).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
govulncheck_version="${GOVULNCHECK_VERSION:-v1.9.0}"
vulndb_url="${GOVULNDB_URL:-https://vuln.go.dev/vulndb.zip}"
vulndb_dir="${GOVULNDB_DIR:-${HOME}/.cache/forma-govulndb}"
attempts="${AUDIT_ATTEMPTS:-3}"
accepted_file="${repo_root}/apps/server/govulncheck-accepted.txt"

annotate() {
  local level=$1
  shift
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::${level}::$*"
  else
    echo "${level}: $*" >&2
  fi
}

# retry runs its arguments until they succeed, at most AUDIT_ATTEMPTS times.
retry() {
  local i
  for ((i = 1; i <= attempts; i++)); do
    if "$@"; then
      return 0
    fi
    if ((i < attempts)); then
      echo "Attempt ${i} of ${attempts} failed: $*; retrying in $((i * 10)) s." >&2
      sleep $((i * 10))
    fi
  done
  return 1
}

# fetch_vulndb unpacks a fresh copy of the Go vulnerability database into vulndb_dir, or keeps the copy already there when the download fails. Fails when there is neither.
fetch_vulndb() {
  local work
  work="$(mktemp -d)"
  if retry curl -fsSL --max-time 300 -o "${work}/vulndb.zip" "${vulndb_url}" && unzip -q "${work}/vulndb.zip" -d "${work}/db" && [[ -f "${work}/db/index/db.json" ]]; then
    rm -rf "${vulndb_dir}"
    mkdir -p "$(dirname "${vulndb_dir}")"
    mv "${work}/db" "${vulndb_dir}"
  elif [[ -f "${vulndb_dir}/index/db.json" ]]; then
    annotate warning "Could not download the Go vulnerability database from ${vulndb_url}; scanning with the cached copy, last modified $(jq -r '.modified' "${vulndb_dir}/index/db.json")."
  else
    rm -rf "${work}"
    return 1
  fi
  rm -rf "${work}"
}

scan_go() {
  cd "${repo_root}/apps/server"
  if ! command -v go >/dev/null 2>&1; then
    annotate warning "Go is not set up; govulncheck skipped."
    return 0
  fi
  if ! retry go install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"; then
    annotate warning "Could not install govulncheck ${govulncheck_version}; the Go vulnerability scan was skipped."
    return 0
  fi
  local gobin
  gobin="$(go env GOBIN)"
  local govulncheck="${gobin:-$(go env GOPATH)/bin}/govulncheck"
  if ! retry go mod download; then
    annotate warning "Could not download the server's modules; the Go vulnerability scan was skipped."
    return 0
  fi
  if ! fetch_vulndb; then
    annotate warning "Could not download the Go vulnerability database and no cached copy exists; the Go vulnerability scan was skipped."
    return 0
  fi

  local report
  report="$(mktemp)"
  # With -format json govulncheck exits 0 whatever it finds; a non-zero exit is a scan that did not run (the code does not load), which fails the gate.
  "${govulncheck}" -db "file://${vulndb_dir}" -format json ./... >"${report}"

  # A finding whose innermost frame names a function is one the code reaches; the others stop at the package or the module.
  local reached accepted unaccepted stale
  reached="$(jq -r 'select(.finding != null) | .finding | select((.trace[0].function // "") != "") | .osv' "${report}" | sort -u)"
  accepted="$({ grep -vE '^[[:space:]]*(#|$)' "${accepted_file}" || true; } | awk '{ print $1 }' | sort -u)"
  unaccepted="$(comm -23 <(printf '%s\n' "${reached}" | sed '/^$/d') <(printf '%s\n' "${accepted}" | sed '/^$/d'))"
  stale="$(comm -13 <(printf '%s\n' "${reached}" | sed '/^$/d') <(printf '%s\n' "${accepted}" | sed '/^$/d'))"
  rm -f "${report}"

  # The readable report, call traces included, for the log.
  "${govulncheck}" -db "file://${vulndb_dir}" ./... || true

  for id in ${stale}; do
    annotate warning "${id} is listed in apps/server/govulncheck-accepted.txt but the server no longer reaches it; remove the entry."
  done
  for id in $(comm -12 <(printf '%s\n' "${reached}" | sed '/^$/d') <(printf '%s\n' "${accepted}" | sed '/^$/d')); do
    echo "${id}: reached, accepted in apps/server/govulncheck-accepted.txt."
  done
  if [[ -n "${unaccepted}" ]]; then
    for id in ${unaccepted}; do
      annotate error "The server reaches ${id} (https://pkg.go.dev/vuln/${id}). Upgrade the module to its fixed version, or, if the vulnerable path cannot be exploited here, record why in apps/server/govulncheck-accepted.txt."
    done
    return 1
  fi
  echo "govulncheck: no vulnerability the server reaches beyond the accepted ones."
}

scan_npm() {
  cd "${repo_root}"
  if ! command -v pnpm >/dev/null 2>&1; then
    annotate warning "pnpm is not set up; pnpm audit skipped."
    return 0
  fi
  local report i
  report="$(mktemp)"
  # pnpm audit answers {"error": ...} when the registry cannot be reached, and a report with "metadata" otherwise; its exit status says only whether something was found.
  for ((i = 1; i <= attempts; i++)); do
    pnpm audit --prod --json >"${report}" 2>/dev/null || true
    if jq -e 'has("metadata")' "${report}" >/dev/null 2>&1; then
      break
    fi
    if ((i < attempts)); then
      echo "pnpm audit attempt ${i} of ${attempts} got no report; retrying in $((i * 10)) s." >&2
      sleep $((i * 10))
    fi
  done
  if ! jq -e 'has("metadata")' "${report}" >/dev/null 2>&1; then
    annotate warning "pnpm audit could not reach the registry ($(jq -r '.error.message? // "no report"' "${report}" 2>/dev/null || echo "no report")); the npm vulnerability scan was skipped."
    rm -f "${report}"
    return 0
  fi
  local severe
  severe="$(jq '[.advisories[]? | select(.severity == "high" or .severity == "critical")] | length' "${report}")"
  jq -r '"pnpm audit (production dependencies): \(.metadata.vulnerabilities | to_entries | map("\(.value) \(.key)") | join(", "))"' "${report}"
  if ((severe > 0)); then
    jq -r '.advisories[]? | select(.severity == "high" or .severity == "critical") | "\(.severity) \(.module_name) \(.vulnerable_versions): \(.title) \(.url)"' "${report}" |
      while IFS= read -r line; do annotate error "${line}"; done
    rm -f "${report}"
    return 1
  fi
  rm -f "${report}"
}

case "${1:-}" in
  go) scan_go ;;
  npm) scan_npm ;;
  *)
    echo "usage: $0 go|npm" >&2
    exit 2
    ;;
esac
