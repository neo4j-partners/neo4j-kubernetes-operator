#!/usr/bin/env bash
# scope/apply-unwatched — create a namespace the operator does NOT watch and apply
# a valid Neo4j CR into it. Used by the p6-scope suite to prove single-namespace
# scope (OP-2-001-SCOPE-01): the operator watches only WATCH_NAMESPACE
# ("default,neo4j-operator-system"), so a CR here must be left untouched.
#
# The manifest is inlined (not a fixtures/ file) because it is only ever applied
# to the unwatched namespace and must not be picked up by the normal deploy path.
#
# Shared defaults (same fallbacks in verify.sh and cleanup-unwatched/run.sh):
#   E2E_SCOPE_NAMESPACE — unwatched namespace          (default e2e-unwatched)
#   E2E_SCOPE_CR        — CR name inside that namespace (default e2e-scope)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NS="${E2E_SCOPE_NAMESPACE:-e2e-unwatched}"
CR="${E2E_SCOPE_CR:-e2e-scope}"

log "Creating unwatched namespace ${NS}"
kubectl create namespace "${NS}" --dry-run=client -o yaml | kubectl apply -f -

log "Applying Neo4j CR ${CR} into unwatched namespace ${NS}"
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

log "CR ${CR} applied in ${NS} (operator should ignore it)"
