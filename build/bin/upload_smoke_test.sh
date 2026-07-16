#!/bin/sh
# Smoke tests for build/bin/upload obfuscation and SFTP credential gating.
#
# Test matrix:
#   A) obfuscate=true, no SFTP creds  → exit 0 after obfuscation (skip upload)
#   B) obfuscate unset, no SFTP creds → exit 1 (legacy requires creds)
#   C) obfuscate=true, SFTP creds set → obfuscation + tar + stub SFTP succeed
#
# Uses sed-patched copies of upload with temp paths and stub binaries so the
# production script is not modified.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/../.." && pwd)
UPLOAD_SRC="${REPO_ROOT}/build/bin/upload"

PASS_COUNT=0
FAIL_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  echo "PASS: $*"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  echo "FAIL: $*" >&2
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

create_stub_binaries() {
  stub_root="$1"
  mkdir -p "${stub_root}"

  cat > "${stub_root}/must-gather-operator" <<'EOF'
#!/bin/sh
output=""
while [ $# -gt 0 ]; do
  case "$1" in
    obfuscate) shift ;;
    --output) output="$2"; shift 2 ;;
    --input|--config) shift 2 ;;
    -v=*) shift ;;
    *) shift ;;
  esac
done
if [ -z "${output}" ]; then
  echo "stub obfuscate: missing --output" >&2
  exit 1
fi
mkdir -p "${output}"
printf 'report: smoke\n' > "${output}/report.yaml"
exit 0
EOF
  chmod +x "${stub_root}/must-gather-operator"

  cat > "${stub_root}/sshpass" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "${stub_root}/sshpass"

  cat > "${stub_root}/sftp" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "${stub_root}/sftp"
}

prepare_upload_script() {
  workdir="$1"
  upload_root="$2"
  stub_root="$3"

  sed \
    -e "s|/usr/local/bin/must-gather-operator|${stub_root}/must-gather-operator|g" \
    -e "s|/must-gather-upload|${upload_root}|g" \
    -e "s|/tmp/must-gather-operator|${workdir}/must-gather-operator|g" \
    "${UPLOAD_SRC}" > "${workdir}/upload-under-test.sh"
  chmod +x "${workdir}/upload-under-test.sh"
}

setup_fixture_dirs() {
  workdir="$1"
  upload_root="$2"
  input_dir="${workdir}/input"
  mkdir -p "${input_dir}" "${upload_root}"
  printf 'contact 10.0.0.1\n' > "${input_dir}/sample.txt"
}

run_case_a_obfuscate_only() {
  workdir=$(mktemp -d)
  upload_root="${workdir}/upload-vol"
  stub_root="${workdir}/stubs"
  trap 'rm -rf "${workdir}"' EXIT INT HUP

  create_stub_binaries "${stub_root}"
  prepare_upload_script "${workdir}" "${upload_root}" "${stub_root}"
  setup_fixture_dirs "${workdir}" "${upload_root}"

  set +e
  output=$( \
    obfuscate=true \
    must_gather_output="${workdir}/input" \
    must_gather_upload="${upload_root}" \
    "${workdir}/upload-under-test.sh" 2>&1 \
  )
  status=$?
  set -e

  if [ "${status}" -ne 0 ]; then
    fail "case A expected exit 0, got ${status}; output: ${output}"
    return
  fi
  if ! echo "${output}" | grep -q "skipping upload"; then
    fail "case A expected skip-upload message; output: ${output}"
    return
  fi
  if [ ! -f "${upload_root}/cleaned/report.yaml" ]; then
    fail "case A expected report.yaml in cleaned output"
    return
  fi
  pass "case A obfuscate-only without SFTP creds exits 0 with cleaned output"
}

run_case_b_legacy_requires_creds() {
  workdir=$(mktemp -d)
  upload_root="${workdir}/upload-vol"
  stub_root="${workdir}/stubs"
  trap 'rm -rf "${workdir}"' EXIT INT HUP

  create_stub_binaries "${stub_root}"
  prepare_upload_script "${workdir}" "${upload_root}" "${stub_root}"
  setup_fixture_dirs "${workdir}" "${upload_root}"

  set +e
  output=$( \
    must_gather_output="${workdir}/input" \
    must_gather_upload="${upload_root}" \
    "${workdir}/upload-under-test.sh" 2>&1 \
  )
  status=$?
  set -e

  if [ "${status}" -ne 1 ]; then
    fail "case B expected exit 1, got ${status}; output: ${output}"
    return
  fi
  if ! echo "${output}" | grep -q "Required Parameters have not been provided"; then
    fail "case B expected legacy credential error; output: ${output}"
    return
  fi
  pass "case B legacy upload without obfuscate requires SFTP creds"
}

run_case_c_obfuscate_and_upload() {
  workdir=$(mktemp -d)
  upload_root="${workdir}/upload-vol"
  stub_root="${workdir}/stubs"
  trap 'rm -rf "${workdir}"' EXIT INT HUP

  create_stub_binaries "${stub_root}"
  prepare_upload_script "${workdir}" "${upload_root}" "${stub_root}"
  setup_fixture_dirs "${workdir}" "${upload_root}"

  set +e
  output=$( \
    PATH="${stub_root}:${PATH}" \
    obfuscate=true \
    caseid=12345 \
    username=testuser \
    password=testpass \
    must_gather_output="${workdir}/input" \
    must_gather_upload="${upload_root}" \
    "${workdir}/upload-under-test.sh" 2>&1 \
  )
  status=$?
  set -e

  if [ "${status}" -ne 0 ]; then
    fail "case C expected exit 0, got ${status}; output: ${output}"
    return
  fi
  if ! echo "${output}" | grep -q "Successfully uploaded"; then
    fail "case C expected successful upload message; output: ${output}"
    return
  fi
  if ! ls "${upload_root}"/*.tar.gz >/dev/null 2>&1; then
    fail "case C expected tar.gz archive in upload staging dir"
    return
  fi
  pass "case C obfuscate with SFTP creds runs obfuscation, tar, and upload"
}

main() {
  require_cmd sed
  require_cmd mktemp

  echo "Verifying upload script syntax..."
  bash -n "${UPLOAD_SRC}"

  echo "Running upload smoke test matrix..."
  run_case_a_obfuscate_only
  run_case_b_legacy_requires_creds
  run_case_c_obfuscate_and_upload

  echo ""
  echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"
  if [ "${FAIL_COUNT}" -ne 0 ]; then
    exit 1
  fi
}

main "$@"
