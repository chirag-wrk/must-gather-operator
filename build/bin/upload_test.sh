#!/bin/bash
# Shell tests for build/bin/upload obfuscation integration (MG-293 Phase 4).
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
UPLOAD_SCRIPT="${SCRIPT_DIR}/upload"
TEST_ROOT=""
BIN_DIR=""
PASS_COUNT=0
FAIL_COUNT=0

cleanup() {
  if [ -n "${TEST_ROOT}" ] && [ -d "${TEST_ROOT}" ]; then
    rm -rf "${TEST_ROOT}"
  fi
}
trap cleanup EXIT

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  echo "PASS: $1"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  echo "FAIL: $1" >&2
}

assert_eq() {
  local got=$1
  local want=$2
  local msg=$3
  if [ "${got}" = "${want}" ]; then
    pass "${msg}"
  else
    fail "${msg} (got=${got}, want=${want})"
  fi
}

assert_contains() {
  local haystack=$1
  local needle=$2
  local msg=$3
  if echo "${haystack}" | grep -F -- "${needle}" >/dev/null; then
    pass "${msg}"
  else
    fail "${msg} (missing: ${needle})"
  fi
}

assert_not_contains() {
  local haystack=$1
  local needle=$2
  local msg=$3
  if echo "${haystack}" | grep -F -- "${needle}" >/dev/null; then
    fail "${msg} (unexpected: ${needle})"
  else
    pass "${msg}"
  fi
}

log_lines() {
  local file=$1
  if [ ! -f "${file}" ]; then
    echo 0
  else
    wc -l < "${file}" | tr -d ' '
  fi
}

init_test_env() {
  TEST_ROOT=$(mktemp -d)
  BIN_DIR="${TEST_ROOT}/bin"
  mkdir -p "${BIN_DIR}"
}

write_obfuscate_stub() {
  cat > "${BIN_DIR}/must-gather-operator" << 'EOF'
#!/bin/sh
echo "$*" >> "${OBFUSCATE_INVOCATION_LOG:-/dev/null}"
if [ "$1" != "obfuscate" ]; then
  exit 1
fi
if [ "${OBFUSCATE_STUB_EXIT:-0}" != "0" ]; then
  exit "${OBFUSCATE_STUB_EXIT}"
fi
input=""
output=""
while [ $# -gt 0 ]; do
  case "$1" in
    --input) input="$2"; shift 2 ;;
    --output) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "${output}"
if [ -d "${input}" ]; then
  cp -a "${input}/." "${output}/"
else
  echo stub > "${output}/stub.txt"
fi
exit 0
EOF
  chmod +x "${BIN_DIR}/must-gather-operator"
}

write_upload_stubs() {
  cat > "${BIN_DIR}/sshpass" << 'EOF'
#!/bin/sh
shift
exec "$@"
EOF
  cat > "${BIN_DIR}/sftp" << 'EOF'
#!/bin/sh
echo "sftp invoked" >> "${SFTP_INVOCATION_LOG:-/dev/null}"
exit 0
EOF
  chmod +x "${BIN_DIR}/sshpass" "${BIN_DIR}/sftp"
}

prepare_fixture() {
  local raw_dir=$1
  mkdir -p "${raw_dir}"
  echo "fixture-content" > "${raw_dir}/cluster.log"
  echo "secret-data" > "${raw_dir}/secret.yaml"
}

run_upload() {
  local env_file=$1
  unset obfuscate obfuscate_config caseid username password host internal_user \
    http_proxy https_proxy no_proxy OBFUSCATE_STUB_EXIT || true
  # shellcheck disable=SC1090
  set -a
  # shellcheck source=/dev/null
  . "${env_file}"
  set +a
  export PATH="${BIN_DIR}:${PATH}"
  /bin/sh "${UPLOAD_SCRIPT}" 2>&1 || true
}

write_env_file() {
  local path=$1
  shift
  : > "${path}"
  while [ $# -gt 0 ]; do
    printf '%s\n' "$1" >> "${path}"
    shift
  done
}

test_obfuscate_unset_requires_credentials() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "SFTP_INVOCATION_LOG=${TEST_ROOT}/sftp.log" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test"

  local output
  output=$(run_upload "${env_file}")
  local exit_code=${PIPESTATUS[0]:-0}

  assert_eq "0" "$(log_lines "${TEST_ROOT}/obfuscate.log")" "obfuscate unset skips obfuscation invocation"
  assert_contains "${output}" "Required Parameters have not been provided" "obfuscate unset preserves credential check"
  assert_eq "0" "$(log_lines "${TEST_ROOT}/sftp.log")" "obfuscate unset does not invoke SFTP without credentials"
}

test_obfuscate_enabled_logs_and_invokes() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "SFTP_INVOCATION_LOG=${TEST_ROOT}/sftp.log" \
    "obfuscate=true" \
    "caseid=1234" \
    "username=user" \
    "password=pass" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test" \
    "host=example.com"

  local output
  output=$(run_upload "${env_file}")

  assert_contains "${output}" "Running obfuscation" "obfuscate=true logs Running obfuscation"
  assert_contains "${output}" "Obfuscation complete" "obfuscate=true logs Obfuscation complete"
  assert_contains "$(cat "${TEST_ROOT}/obfuscate.log")" "--input ${raw}" "stub receives --input path"
  assert_contains "$(cat "${TEST_ROOT}/obfuscate.log")" "--output ${TEST_ROOT}/upload/cleaned" "stub receives --output cleaned path"
}

test_obfuscate_failure_skips_tar() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "OBFUSCATE_STUB_EXIT=1" \
    "obfuscate=true" \
    "caseid=1234" \
    "username=user" \
    "password=pass" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test" \
    "host=example.com"

  local output
  output=$(run_upload "${env_file}")

  assert_not_contains "${output}" "Archiving files from" "obfuscate failure skips tar"
  assert_eq "0" "$(log_lines "${TEST_ROOT}/sftp.log")" "obfuscate failure skips SFTP"
}

test_obfuscate_config_forwarded() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "obfuscate=true" \
    "obfuscate_config=/custom/config.yaml" \
    "caseid=1234" \
    "username=user" \
    "password=pass" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test" \
    "host=example.com"

  run_upload "${env_file}" >/dev/null
  assert_contains "$(cat "${TEST_ROOT}/obfuscate.log")" "--config /custom/config.yaml" "obfuscate_config forwarded to --config"
}

test_obfuscate_only_skips_sftp() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local input_checksum
  input_checksum=$(find "${raw}" -type f -exec md5sum {} + | sort | md5sum | awk '{print $1}')
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "SFTP_INVOCATION_LOG=${TEST_ROOT}/sftp.log" \
    "obfuscate=true" \
    "caseid=" \
    "username=" \
    "password=" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test"

  local output
  output=$(run_upload "${env_file}")

  assert_contains "${output}" "no upload target configured" "obfuscate-only exits after obfuscation"
  assert_eq "0" "$(log_lines "${TEST_ROOT}/sftp.log")" "obfuscate-only does not invoke SFTP"
  local after_checksum
  after_checksum=$(find "${raw}" -type f -exec md5sum {} + | sort | md5sum | awk '{print $1}')
  assert_eq "${input_checksum}" "${after_checksum}" "input fixture unchanged after obfuscate-only (FR-010)"
}

test_obfuscate_upload_tar_from_cleaned() {
  init_test_env
  write_obfuscate_stub
  write_upload_stubs
  local raw="${TEST_ROOT}/raw"
  prepare_fixture "${raw}"
  local env_file="${TEST_ROOT}/env"
  write_env_file "${env_file}" \
    "OBFUSCATE_INVOCATION_LOG=${TEST_ROOT}/obfuscate.log" \
    "SFTP_INVOCATION_LOG=${TEST_ROOT}/sftp.log" \
    "obfuscate=true" \
    "caseid=1234" \
    "username=user" \
    "password=pass" \
    "must_gather_output=${raw}" \
    "must_gather_upload=${TEST_ROOT}/upload" \
    "FILENAME_PREFIX=test" \
    "host=example.com"

  local output
  output=$(run_upload "${env_file}")

  assert_contains "${output}" "Archiving files from ${TEST_ROOT}/upload/cleaned" "tar sources cleaned staging directory"
  assert_contains "${output}" "Successfully uploaded" "obfuscate+upload completes SFTP path"
  assert_eq "1" "$(log_lines "${TEST_ROOT}/sftp.log")" "obfuscate+upload invokes SFTP"
}

main() {
  if [ ! -f "${UPLOAD_SCRIPT}" ]; then
    echo "upload script not found: ${UPLOAD_SCRIPT}" >&2
    exit 1
  fi
  bash -n "${UPLOAD_SCRIPT}"

  test_obfuscate_unset_requires_credentials
  test_obfuscate_enabled_logs_and_invokes
  test_obfuscate_failure_skips_tar
  test_obfuscate_config_forwarded
  test_obfuscate_only_skips_sftp
  test_obfuscate_upload_tar_from_cleaned

  echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"
  if [ "${FAIL_COUNT}" -ne 0 ]; then
    exit 1
  fi
}

main "$@"
