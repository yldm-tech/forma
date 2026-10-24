#!/usr/bin/env bash

# Validates the canonical Forma authorization schema (schema.zed) against the assertion suite in schema-validation.yaml using `zed validate`. It runs fully offline: no SpiceDB server is needed.
#
# ZED_IMAGE below is the only pin of the zed version in the repository (ZED_IMAGE_REF overrides it). Whenever a Docker daemon is reachable the suite runs in that image, so a local run validates with the same zed as CI. Only without Docker does it fall back to a local `zed` binary, whose version may differ from the pin, and it says so.

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly VALIDATION_FILE="schema-validation.yaml"
readonly ZED_IMAGE="${ZED_IMAGE_REF:-authzed/zed:v1.1.1}"

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  docker run --rm \
    --volume "${SCRIPT_DIR}:/authzed:ro" \
    "${ZED_IMAGE}" \
    validate "/authzed/${VALIDATION_FILE}"
elif command -v zed >/dev/null 2>&1; then
  printf 'Docker is not available; validating with the local zed (%s) instead of the pinned %s that CI validates with.\n' "$(command -v zed)" "${ZED_IMAGE}" >&2
  zed validate "${SCRIPT_DIR}/${VALIDATION_FILE}"
else
  printf '%s\n' "Neither Docker nor a zed binary is available. Start Docker (the pinned ${ZED_IMAGE} image is used) or install zed (https://github.com/authzed/zed)." >&2
  exit 1
fi
