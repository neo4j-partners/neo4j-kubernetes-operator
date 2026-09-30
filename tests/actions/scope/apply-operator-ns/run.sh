#!/usr/bin/env bash
# scope/apply-operator-ns — negative control for the cluster-wide suite. Cluster-wide watches every
# namespace, so the boundary that must still hold is NEO-016: a Neo4j CR in the operator's own
# namespace must be ignored (cache field selector + reconciler guard). Apply one there so
# assert/operator-ns-ignored can prove it is left untouched.
#
# Inputs:
#   OPERATOR_HELM_NAMESPACE       — operator namespace (must not be reconciled)
#   E2E_SCOPE_OPERATOR_NS_CR      — CR name in that namespace (default e2e-scope-operatorns)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${OPERATOR_HELM_NAMESPACE:-neo4j-operator-scope}"
CR="${E2E_SCOPE_OPERATOR_NS_CR:-e2e-scope-operatorns}"

log "Applying Neo4j CR ${CR} into operator namespace ${NS} (must be ignored, NEO-016)"
kubectl apply -n "${NS}" -f - <<EOF
apiVersion: neo4j.com/v1
kind: Neo4j
metadata:
  name: ${CR}
spec:
  edition: enterprise
  version: "${NEO4J_VERSION}"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
EOF

log "CR ${CR} applied in operator namespace ${NS}"
