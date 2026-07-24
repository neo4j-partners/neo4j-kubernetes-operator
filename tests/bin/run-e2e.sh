#!/usr/bin/env bash
# Entry point for e2e tests.
# Usage: CLOUD=local-kind E2E_PROFILE=happy-path ./tests/bin/run-e2e.sh [suite]

set -euo pipefail

BIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TESTS_DIR="$(cd "${BIN_DIR}/.." && pwd)"

CLOUD="${CLOUD:-local-kind}"
E2E_PROFILE="${E2E_PROFILE:-happy-path}"
SUITE="${1:-p0-standalone}"

# shellcheck source=../lib/common.sh
source "${TESTS_DIR}/lib/common.sh"
# shellcheck source=../config/load.sh
source "${TESTS_DIR}/config/load.sh"

require_cmd kubectl bash awk

# Fail once when Azure prerequisites are missing.
_require_e2e_cloud_ready() {
  load_cloud_config "${CLOUD}"
  if [[ "${CLOUD_ID:-}" == "azure-aks" && -z "${OPERATOR_IMAGE:-}" ]]; then
    die "OPERATOR_IMAGE is required for CLOUD=azure-aks. Run: make test-e2e-azure  (or: source tests/azure/ensure-aks.sh && bash tests/azure/push-operator-image.sh)"
  fi
}

_require_e2e_cloud_ready

SUITE_FILE="${TESTS_DIR}/suites/${SUITE}.yaml"
[[ -f "${SUITE_FILE}" ]] || die "e2e suite not found: tests/suites/${SUITE}.yaml"

_run_e2e_suite() {
  export CLOUD CLOUD_ID OPERATOR_IMAGE STORAGE_CLASS_NAME \
    OPERATOR_IMAGE_PULL_POLICY OPERATOR_LEADER_ELECT KIND_CLUSTER_NAME \
    E2E_PROFILE E2E_CONFIG_SUMMARY NEO4J_CASE OPERATOR_CASE E2E_EXPAND_MATRIX

  bash "${TESTS_DIR}/runner/run-suite.sh" "${SUITE}"
}

if [[ "${E2E_PROFILE}" == "matrix" ]]; then
  export E2E_EXPAND_MATRIX=true
  load_cloud_config "${CLOUD}"
  _run_e2e_suite
else
  reconcile_config "${E2E_PROFILE}" "${CLOUD}"
  export E2E_EXPAND_MATRIX=false
  _run_e2e_suite
fi
