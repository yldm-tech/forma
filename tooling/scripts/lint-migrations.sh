#!/usr/bin/env bash
# Lints goose migrations with Squawk (server/.squawk.toml), forma2's `lint:migrations`. Pass the migration files to lint; with none, it lints the migrations added or changed since the git revision in BASE_REF (default: the first parent of HEAD, which on a pull request's merge commit is the base branch).
#
# Only the Up section is linted: it is what runs against a live database. The Down section undoes it and is destructive by design (DROP TABLE, DROP COLUMN), so linting it would flag every migration. Goose runs a migration in a transaction unless the file says `-- +goose NO TRANSACTION`, and Squawk is told which, since a statement such as CREATE INDEX CONCURRENTLY is only valid outside one.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config="${repo_root}/server/.squawk.toml"
squawk="${repo_root}/node_modules/.bin/squawk"

files=()
if (($# > 0)); then
  files=("$@")
else
  base_ref="${BASE_REF:-HEAD^1}"
  while IFS= read -r -d '' file; do
    files+=("${repo_root}/${file}")
  done < <(git -C "${repo_root}" diff --diff-filter=ACMR --name-only -z "${base_ref}" HEAD -- 'server/migrations/*.sql')
  if ((${#files[@]} == 0)); then
    echo "No added or changed migrations to lint."
    exit 0
  fi
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

status=0
for file in "${files[@]}"; do
  name="$(basename "${file}")"
  if ! grep -q '^-- +goose Up' "${file}"; then
    echo "${name}: no '-- +goose Up' annotation, so goose would not apply it." >&2
    status=1
    continue
  fi
  # Everything before `-- +goose Down`, keeping line numbers so findings point at the real file.
  awk '/^-- \+goose Down/ { exit } { print }' "${file}" >"${work}/${name}"
  args=(--config "${config}")
  if ! grep -q '^-- +goose NO TRANSACTION' "${file}"; then
    args+=(--assume-in-transaction)
  fi
  echo "Linting ${name}"
  if ! (cd "${work}" && "${squawk}" "${args[@]}" "${name}"); then
    status=1
  fi
done
exit "${status}"
