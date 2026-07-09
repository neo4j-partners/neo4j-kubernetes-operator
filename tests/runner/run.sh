#!/usr/bin/env bash
# Execute one or more e2e scenarios (ADR-012 estate 2).

set -euo pipefail

RUNNER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTS_DIR="$(cd "${RUNNER_DIR}/.." && pwd)"
# shellcheck source=../lib/common.sh
source "${TESTS_DIR}/lib/common.sh"

SCENARIO="${1:-p0-standalone}"
SCENARIO_FILE="${TESTS_DIR}/scenarios/${SCENARIO}.yaml"

[[ -f "${SCENARIO_FILE}" ]] || die "scenario not found: ${SCENARIO}"
require_cmd kubectl awk

RUN_ID="${RUN_ID:-$(date -u +%Y%m%d-%H%M%S)-${CLOUD_ID:-unknown}}"
export RUN_ID

log "Running scenario ${SCENARIO} (cloud=${CLOUD_ID:-unset}, run_id=${RUN_ID})"

failed=0
run_steps() {
  local step
  while IFS= read -r step; do
    [[ -n "${step}" ]] || continue
    if ! run_action "${step}"; then
      failed=1
      return 1
    fi
  done
}

if ! run_steps < <(parse_scenario_list steps "${SCENARIO_FILE}"); then
  collect_diagnostics "${RUN_ID}"
fi

log "Running cleanup"
while IFS= read -r step; do
  [[ -n "${step}" ]] || continue
  bash "${TESTS_DIR}/actions/${step}/run.sh" || true
done < <(parse_scenario_list cleanup "${SCENARIO_FILE}")

if [[ "${failed}" -ne 0 ]]; then
  die "scenario ${SCENARIO} failed (see ${TESTS_DIR}/results/runs/${RUN_ID})"
fi

log "scenario ${SCENARIO} passed"
