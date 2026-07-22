#!/bin/bash
# Verify operator image ships default obfuscation config (MG-293 Phase 5, T5_5).
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)
DEFAULT_CONFIG_PATH="/etc/must-gather-clean/default-config.yaml"
CONTAINER_ENGINE=${CONTAINER_ENGINE:-$(command -v podman 2>/dev/null || command -v docker)}
IMAGE=${IMAGE:-must-gather-operator:mg-293-phase5-test}
BUILD=${BUILD:-false}

if [ -z "${CONTAINER_ENGINE}" ]; then
  echo "CONTAINER_ENGINE not set and neither podman nor docker found" >&2
  exit 1
fi

if [ "${BUILD}" = "true" ]; then
  echo "Building image ${IMAGE} from ${REPO_ROOT}..."
  (cd "${REPO_ROOT}" && ALLOW_DIRTY_CHECKOUT=true ${CONTAINER_ENGINE} build --pull=false -f build/Dockerfile -t "${IMAGE}" .)
fi

echo "Checking default config at ${DEFAULT_CONFIG_PATH} in ${IMAGE}..."
${CONTAINER_ENGINE} run --rm --entrypoint test "${IMAGE}" -f "${DEFAULT_CONFIG_PATH}"

echo "Config preview:"
${CONTAINER_ENGINE} run --rm --entrypoint head "${IMAGE}" -n 5 "${DEFAULT_CONFIG_PATH}"

echo "Checking obfuscate subcommand..."
${CONTAINER_ENGINE} run --rm "${IMAGE}" obfuscate --help >/dev/null

echo "PASS: image ${IMAGE} contains default obfuscation config and obfuscate CLI"
