#!/usr/bin/env bash
# scope/apply-watched — positive control for the scope suite (AC-OP-SCOPE-SINGLE-002):
# apply a valid Neo4j CR into a WATCHED namespace (NEO4J_NAMESPACE, default "default")
# so assert/reconciled-in-namespace can confirm the operator DOES reconcile it.
# Paired with the negative control (scope/apply-unwatched) this proves the scope
# boundary in both directions within one suite.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${NEO4J_NAMESPACE:-default}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"

log "Applying Neo4j CR ${CR} into watched namespace ${NS} (operator should reconcile it)"
kubectl apply -n "${NS}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: ${CR}
spec:
  edition: enterprise
  version: "2026.05.0"
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

log "CR ${CR} applied in watched namespace ${NS}"
