#!/usr/bin/env bash
# Shared helpers for the e2e harness (ADR-012 estate 2).

set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${TESTS_DIR}/.." && pwd)"

log() {
  printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"
}

die() {
  log "ERROR: $*"
  exit 1
}

require_cmd() {
  local cmd
  for cmd in "$@"; do
    command -v "${cmd}" >/dev/null 2>&1 || die "required command not found: ${cmd}"
  done
}

kubectl_wait_deployment() {
  local namespace=$1
  local name=$2
  local timeout=${3:-180s}
  kubectl rollout status "deployment/${name}" -n "${namespace}" --timeout="${timeout}"
}

kubectl_jsonpath() {
  kubectl get "$1" -n "${NEO4J_NAMESPACE}" -o "jsonpath={$2}"
}

resolve_action_template() {
  local template=$1
  local resolved="${template//\{\{assert\}\}/${SUITE_CASE_ASSERT:-}}"
  resolved="${resolved//\{\{id\}\}/${SUITE_CASE_ID:-}}"
  printf '%s' "${resolved}"
}

run_action() {
  local action=$1
  local mode=${2:-full}
  local action_dir="${TESTS_DIR}/actions/${action}"
  [[ -d "${action_dir}" ]] || die "unknown action: ${action}"

  case "${mode}" in
    full)
      log "ACTION run: ${action}"
      bash "${action_dir}/run.sh"
      log "ACTION verify: ${action}"
      bash "${action_dir}/verify.sh"
      ;;
    run)
      log "ACTION run: ${action}"
      bash "${action_dir}/run.sh"
      ;;
    verify)
      log "ACTION verify: ${action}"
      bash "${action_dir}/verify.sh"
      ;;
    *)
      die "unknown run_action mode: ${mode}"
      ;;
  esac
}

run_phase() {
  local label=$1
  local mode=$2
  shift 2
  local step resolved

  [[ "$#" -gt 0 ]] || return 0

  log "PHASE ${label}"
  for step in "$@"; do
    [[ -n "${step}" ]] || continue
    resolved="$(resolve_action_template "${step}")"
    run_action "${resolved}" "${mode}"
  done
}

run_cleanup_phase() {
  local label=$1
  shift
  local step resolved

  [[ "$#" -gt 0 ]] || return 0

  log "PHASE ${label}"
  for step in "$@"; do
    [[ -n "${step}" ]] || continue
    resolved="$(resolve_action_template "${step}")"
    bash "${TESTS_DIR}/actions/${resolved}/run.sh" || true
  done
}

# Apply-result handoff: actions run in separate `bash` processes, so exported
# vars don't survive between deploy (case_run) and admission asserts (case_assert).
# Persist the last kubectl apply outcome to files keyed by run + case instead.
_apply_state_dir() {
  printf '%s' "${TESTS_DIR}/results/runs/${RUN_ID:-manual}/.apply-state"
}

# record_apply_result <exit_code> <stderr_file> — called by deploy after apply.
record_apply_result() {
  local exit_code=$1
  local stderr_file=$2
  local dir
  dir="$(_apply_state_dir)"
  mkdir -p "${dir}"
  printf '%s' "${exit_code}" >"${dir}/${SUITE_CASE_ID:-case}.exit"
  cp "${stderr_file}" "${dir}/${SUITE_CASE_ID:-case}.stderr" 2>/dev/null \
    || : >"${dir}/${SUITE_CASE_ID:-case}.stderr"
}

read_apply_exit() {
  cat "$(_apply_state_dir)/${SUITE_CASE_ID:-case}.exit" 2>/dev/null || printf ''
}

read_apply_stderr() {
  cat "$(_apply_state_dir)/${SUITE_CASE_ID:-case}.stderr" 2>/dev/null || printf ''
}

collect_diagnostics() {
  local run_id=${1:-manual}
  local out="${TESTS_DIR}/results/runs/${run_id}"
  mkdir -p "${out}"

  log "Collecting diagnostics into ${out}"
  kubectl get crd "${OPERATOR_CRD}" -o yaml >"${out}/crd.yaml" 2>&1 || true
  kubectl get all -n "${OPERATOR_NAMESPACE}" -o wide >"${out}/operator-all.txt" 2>&1 || true
  kubectl describe deployment -n "${OPERATOR_NAMESPACE}" >"${out}/operator-deployment.txt" 2>&1 || true
  kubectl logs -n "${OPERATOR_NAMESPACE}" -l "${OPERATOR_LABEL_SELECTOR}" --tail=200 >"${out}/operator-logs.txt" 2>&1 || true
  kubectl get neo4j,sts,svc,secret,configmap,pvc,pods -n "${NEO4J_NAMESPACE}" -o wide >"${out}/workload.txt" 2>&1 || true
  kubectl describe neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >"${out}/neo4j-describe.txt" 2>&1 || true
}
