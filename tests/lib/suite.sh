#!/usr/bin/env bash
# Suite and pipeline YAML parsing (no yq dependency).

set -euo pipefail

SUITE_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTS_DIR="$(cd "${SUITE_LIB_DIR}/.." && pwd)"

# Print action paths listed under a top-level YAML key (e.g. setup: / case_run:).
parse_pipeline_phase() {
  local file=$1
  local phase=$2
  awk -v phase="${phase}:" '
    { sub(/\r$/, "") }
    $0 == phase { in_phase=1; next }
    in_phase && /^[^[:space:]#-]/ { in_phase=0 }
    in_phase && /^  - / {
      line=$0
      sub(/^  - /, "", line)
      print line
    }
  ' "${file}"
}

# Print actions under pipeline.<phase> in a suite file.
parse_suite_pipeline_phase() {
  local file=$1
  local phase=$2
  awk -v phase="${phase}" '
    { sub(/\r$/, "") }
    /^pipeline:/ { in_pipeline=1; next }
    in_pipeline && /^[^[:space:]#]/ { in_pipeline=0 }
    in_pipeline && $0 == sprintf("  %s:", phase) { in_phase=1; next }
    in_phase && /^  [^ ]/ && $0 !~ /^    - / { in_phase=0 }
    in_phase && /^    - / {
      line=$0
      sub(/^    - /, "", line)
      print line
    }
  ' "${file}"
}

suite_yaml_value() {
  local file=$1
  local key=$2
  awk -v key="${key}:" '
    { sub(/\r$/, "") }
    $1 == key {
      sub(/^[^:]*:[[:space:]]*/, "")
      print
      exit
    }
  ' "${file}"
}

suite_nested_value() {
  local file=$1
  local parent=$2
  local key=$3
  awk -v parent="${parent}:" -v key="${key}:" '
    { sub(/\r$/, "") }
    $0 == parent { in_parent=1; next }
    in_parent && /^[^[:space:]#]/ { in_parent=0 }
    in_parent && index($0, "  " key) == 1 {
      sub(/^[^:]*:[[:space:]]*/, "")
      print
      exit
    }
  ' "${file}"
}

# Merge pipeline phases: suite overrides extend shared pipeline file.
suite_resolve_phase() {
  local suite_file=$1
  local pipeline_file=$2
  local phase=$3

  local -a items=()
  local item
  while IFS= read -r item; do
    [[ -n "${item}" ]] && items+=("${item}")
  done < <(parse_pipeline_phase "${pipeline_file}" "${phase}")

  while IFS= read -r item; do
    [[ -n "${item}" ]] && items+=("${item}")
  done < <(parse_suite_pipeline_phase "${suite_file}" "${phase}")

  if [[ "${#items[@]}" -eq 0 ]]; then
    return 0
  fi
  printf '%s\n' "${items[@]}"
}

suite_load_merged_pipeline() {
  local suite_file=$1
  local pipeline_name
  pipeline_name="$(suite_yaml_value "${suite_file}" "use_pipeline")"
  [[ -n "${pipeline_name}" ]] || die "suite missing use_pipeline: ${suite_file}"

  local pipeline_file="${TESTS_DIR}/pipelines/${pipeline_name}.yaml"
  [[ -f "${pipeline_file}" ]] || die "pipeline not found: ${pipeline_file}"

  SUITE_PIPELINE_FILE="${pipeline_file}"
  export SUITE_PIPELINE_FILE
}

# Emit one TSV row per case: id, fixture, assert, expect, expect_contains, cr_name,
# neo4j_case, operator_case, clouds, from_reconcile
suite_parse_cases() {
  local file=$1
  awk '
    { sub(/\r$/, "") }
    function emit() {
      if (id == "") return
      printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
        id, fixture, assert, expect, expect_contains, cr_name,
        neo4j_case, operator_case, clouds, from_reconcile
    }
    /^cases:/ { in_cases=1; next }
    in_cases && /^[^[:space:]#-]/ { emit(); exit }
    in_cases && /^  - id:/ {
      emit()
      id=$3
      fixture=assert=expect=expect_contains=cr_name=""
      neo4j_case=operator_case=clouds=""
      from_reconcile="false"
      next
    }
    in_cases && /^    fixture:/ { fixture=$2; next }
    in_cases && /^    assert:/ { assert=$2; next }
    in_cases && /^    expect:/ { expect=$2; next }
    in_cases && /^    expect_contains:/ { expect_contains=$2; next }
    in_cases && /^    cr_name:/ { cr_name=$2; next }
    in_cases && /^    neo4j_case:/ { neo4j_case=$2; next }
    in_cases && /^    operator_case:/ { operator_case=$2; next }
    in_cases && /^    clouds:/ {
      clouds=$0
      sub(/^    clouds:[[:space:]]*\[/, "", clouds)
      sub(/\][[:space:]]*$/, "", clouds)
      gsub(/,[[:space:]]*/, ",", clouds)
      next
    }
    in_cases && /^    from_reconcile:/ {
      from_reconcile=$2
      next
    }
    END { emit() }
  ' "${file}"
}

suite_case_allowed_on_cloud() {
  local clouds=$1
  [[ -z "${clouds}" ]] && return 0
  local cloud
  IFS=',' read -r -a _clouds <<<"${clouds}"
  for cloud in "${_clouds[@]}"; do
    cloud="${cloud// /}"
    [[ "${cloud}" == "${CLOUD_ID:-}" ]] && return 0
  done
  return 1
}

apply_suite_case_row() {
  local row=$1

  local id fixture assert expect expect_contains cr_name neo4j_case operator_case clouds from_reconcile
  IFS=$'\t' read -r id fixture assert expect expect_contains cr_name neo4j_case operator_case clouds from_reconcile <<<"${row}"

  if ! suite_case_allowed_on_cloud "${clouds}"; then
    log "SKIP case ${id} (cloud ${CLOUD_ID:-unset} not in [${clouds}])"
    return 2
  fi

  export SUITE_CASE_ID="${id}"
  export SUITE_CASE_ASSERT="${assert}"
  export EXPECT_CONTAINS="${expect_contains}"

  if [[ -n "${operator_case}" && -n "${neo4j_case}" ]]; then
    reconcile_config_for_cases "${CLOUD}" "${operator_case}" "${neo4j_case}" "${E2E_PROFILE:-explicit}"
  elif [[ "${from_reconcile}" == "true" ]]; then
    : "${OPERATOR_CASE:?OPERATOR_CASE not set for from_reconcile case}"
    : "${NEO4J_CASE:?NEO4J_CASE not set for from_reconcile case}"
    reconcile_config_for_cases "${CLOUD}" "${OPERATOR_CASE}" "${NEO4J_CASE}" "${E2E_PROFILE:-happy-path}"
  elif [[ -n "${neo4j_case}" ]]; then
    : "${OPERATOR_CASE:?OPERATOR_CASE not set}"
    reconcile_config_for_cases "${CLOUD}" "${OPERATOR_CASE}" "${neo4j_case}" "${E2E_PROFILE:-happy-path}"
  fi

  if [[ -n "${fixture}" ]]; then
    export NEO4J_STANDALONE_FIXTURE="${fixture}"
    unset NEO4J_USE_STORAGE_CLASS
    if [[ -z "${cr_name}" ]]; then
      export NEO4J_CR_NAME="e2e-${id}"
    else
      export NEO4J_CR_NAME="${cr_name}"
    fi
    # shellcheck source=../config/derive.sh
    source "${TESTS_DIR}/config/derive.sh"
    neo4j_derive_names
  fi

  case "${expect}" in
    failure) export CASE_EXPECT=failure ;;
    success | "") export CASE_EXPECT=success ;;
    *) die "unknown case expect=${expect} in case ${id}" ;;
  esac

  unset LAST_APPLY_EXIT_CODE LAST_APPLY_STDERR
  export E2E_CONFIG_SUMMARY="suite=${SUITE_NAME} case=${id} cloud=${CLOUD_ID} assert=${assert} cr=${NEO4J_CR_NAME:-unset} expect=${CASE_EXPECT}"
}
