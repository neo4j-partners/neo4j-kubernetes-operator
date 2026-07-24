#!/usr/bin/env bash
# Execute an e2e suite: setup once, cases loop, teardown once.

set -euo pipefail

RUNNER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTS_DIR="$(cd "${RUNNER_DIR}/.." && pwd)"
# shellcheck source=../lib/common.sh
source "${TESTS_DIR}/lib/common.sh"
# shellcheck source=../lib/suite.sh
source "${TESTS_DIR}/lib/suite.sh"
# shellcheck source=../config/load.sh
source "${TESTS_DIR}/config/load.sh"

SUITE_NAME="${1:-p0-standalone}"
SUITE_FILE="${TESTS_DIR}/suites/${SUITE_NAME}.yaml"

[[ -f "${SUITE_FILE}" ]] || die "suite not found: ${SUITE_FILE}"
require_cmd kubectl awk

RUN_ID="${RUN_ID:-$(date -u +%Y%m%d-%H%M%S)-${CLOUD_ID:-unknown}}"
export RUN_ID
export SUITE_NAME

ON_CASE_FAILURE="$(suite_yaml_value "${SUITE_FILE}" "on_case_failure")"
ON_CASE_FAILURE="${ON_CASE_FAILURE:-stop}"

suite_clouds="$(suite_yaml_value "${SUITE_FILE}" "clouds")"
suite_clouds="${suite_clouds//[\[\]]/}"
if [[ -n "${suite_clouds}" ]] && ! suite_case_allowed_on_cloud "${suite_clouds}"; then
  die "suite ${SUITE_NAME} is not configured for cloud ${CLOUD_ID:-unset} (allowed: ${suite_clouds})"
fi

suite_load_merged_pipeline "${SUITE_FILE}"

SETUP_STEPS=()
TEARDOWN_STEPS=()
CASE_RUN_STEPS=()
CASE_ASSERT_STEPS=()
CASE_TEARDOWN_STEPS=()

while IFS= read -r line; do
  [[ -n "${line}" ]] && SETUP_STEPS+=("${line}")
done < <(suite_resolve_phase "${SUITE_FILE}" "${SUITE_PIPELINE_FILE}" "setup")

while IFS= read -r line; do
  [[ -n "${line}" ]] && TEARDOWN_STEPS+=("${line}")
done < <(suite_resolve_phase "${SUITE_FILE}" "${SUITE_PIPELINE_FILE}" "teardown")

while IFS= read -r line; do
  [[ -n "${line}" ]] && CASE_RUN_STEPS+=("${line}")
done < <(suite_resolve_phase "${SUITE_FILE}" "${SUITE_PIPELINE_FILE}" "case_run")

while IFS= read -r line; do
  [[ -n "${line}" ]] && CASE_ASSERT_STEPS+=("${line}")
done < <(suite_resolve_phase "${SUITE_FILE}" "${SUITE_PIPELINE_FILE}" "case_assert")

while IFS= read -r line; do
  [[ -n "${line}" ]] && CASE_TEARDOWN_STEPS+=("${line}")
done < <(suite_resolve_phase "${SUITE_FILE}" "${SUITE_PIPELINE_FILE}" "case_teardown")

suite_build_case_rows() {
  local suite_file=$1
  if [[ "${E2E_EXPAND_MATRIX:-false}" == "true" ]]; then
    local assert_name
    assert_name="$(suite_nested_value "${suite_file}" "expand_matrix" "assert")"
    [[ -n "${assert_name}" ]] || die "expand_matrix.assert required in ${suite_file}"
    local op neo4j
    while read -r op neo4j; do
      [[ -n "${op}" ]] || continue
      # US-delimited (\037) row matching suite_parse_cases field order.
      printf 'matrix-%s-%s\037\037%s\037success\037\037\037%s\037%s\037\037false\n' \
        "${op}" "${neo4j}" "${assert_name}" "${neo4j}" "${op}"
    done < <(reconcile_list_combinations "${CLOUD}" "${SUITE_NAME}")
  else
    suite_parse_cases "${suite_file}"
  fi
}

log "Running suite ${SUITE_NAME} (cloud=${CLOUD_ID:-unset}, run_id=${RUN_ID})"

suite_failed=0
if ! run_phase "setup" full "${SETUP_STEPS[@]}"; then
  collect_diagnostics "${RUN_ID}"
  die "suite ${SUITE_NAME} setup failed"
fi

case_rows_file="$(mktemp)"
suite_build_case_rows "${SUITE_FILE}" >"${case_rows_file}"
total_cases="$(wc -l <"${case_rows_file}" | tr -d ' ')"
[[ "${total_cases}" -gt 0 ]] || die "suite ${SUITE_NAME} has no cases to run"

log "Suite ${SUITE_NAME}: ${total_cases} case(s)"

case_idx=0
while IFS= read -r case_row; do
  [[ -n "${case_row}" ]] || continue
  case_idx=$((case_idx + 1))

  apply_rc=0
  apply_suite_case_row "${case_row}" || apply_rc=$?

  if [[ "${apply_rc}" -eq 2 ]]; then
    continue
  fi
  if [[ "${apply_rc}" -ne 0 ]]; then
    suite_failed=1
    [[ "${ON_CASE_FAILURE}" == "continue" ]] && continue
    break
  fi

  log "CASE [${case_idx}/${total_cases}] ${E2E_CONFIG_SUMMARY}"
  case_failed=0

  if ! run_phase "case_run" full "${CASE_RUN_STEPS[@]}"; then
    case_failed=1
  elif [[ "${#CASE_ASSERT_STEPS[@]}" -gt 0 ]]; then
    if ! run_phase "case_assert" full "${CASE_ASSERT_STEPS[@]}"; then
      case_failed=1
    fi
  fi

  if [[ "${case_failed}" -ne 0 ]]; then
    suite_failed=1
    collect_diagnostics "${RUN_ID}-${SUITE_CASE_ID}"
  fi

  run_cleanup_phase "case_teardown" "${CASE_TEARDOWN_STEPS[@]}"

  if [[ "${case_failed}" -ne 0 ]]; then
    [[ "${ON_CASE_FAILURE}" == "continue" ]] && continue
    break
  fi
done <"${case_rows_file}"
rm -f "${case_rows_file}"

run_cleanup_phase "teardown" "${TEARDOWN_STEPS[@]}"

if [[ "${suite_failed}" -ne 0 ]]; then
  die "suite ${SUITE_NAME} failed (see ${TESTS_DIR}/results/runs/)"
fi

log "suite ${SUITE_NAME} passed (${total_cases} case(s))"
