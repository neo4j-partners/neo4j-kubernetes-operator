#!/usr/bin/env bash
# scope/apply-multi — apply the same Neo4j CR into every watched namespace
# (OP-2-001-SCOPE-02). One CR per namespace, same name in each: reconciling both proves the
# operator holds a working cache and working permissions in each entry of WATCH_NAMESPACE, not
# just in the first one.
#
# Inputs:
#   E2E_SCOPE_NAMESPACES — comma-separated watched namespaces
#   E2E_SCOPE_MULTI_CR   — CR name, identical in each namespace (default e2e-scope-multi)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_MULTI_CR:-e2e-scope-multi}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  log "Applying Neo4j CR ${CR} into watched namespace ${ns}"
  kubectl apply -n "${ns}" -f - <<EOF
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
done

log "CR ${CR} applied in ${WATCHED}"
