#!/usr/bin/env bash
# scope/apply-operator-ns (verify) — confirm the CR was accepted into the operator namespace.
# Whether the operator then (correctly) leaves it alone is assert/operator-ns-ignored's job.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CR="${E2E_SCOPE_OPERATOR_NS_CR:-e2e-scope-operatorns}"

kubectl get neo4j "${CR}" -n "${NS}" >/dev/null 2>&1 \
  || die "CR ${CR} was not created in operator namespace ${NS}"

log "CR ${CR} present in operator namespace ${NS}"
