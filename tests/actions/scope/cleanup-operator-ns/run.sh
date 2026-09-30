#!/usr/bin/env bash
# scope/cleanup-operator-ns — remove the negative-control CR that scope/apply-operator-ns placed in
# the operator namespace. Best-effort: teardown runs even on failure. The CR was never reconciled,
# so it carries no finalizer and deletes immediately.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CR="${E2E_SCOPE_OPERATOR_NS_CR:-e2e-scope-operatorns}"

kubectl delete neo4j "${CR}" -n "${NS}" --ignore-not-found --wait=false || true

log "Operator-namespace CR ${CR} cleanup done"
