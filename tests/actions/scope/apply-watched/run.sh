#!/usr/bin/env bash
# scope/apply-watched — positive control for the scope suite: apply the same Neo4j CR into every
# WATCHED namespace, so assert/reconciled-in-namespace can confirm the operator reconciles each
# entry of WATCH_NAMESPACE and not merely the first one. Paired with the negative control
# (scope/apply-unwatched) this proves the scope boundary in both directions within one case.
#
# Inputs:
#   E2E_SCOPE_WATCHED_NAMESPACES — comma-separated watched namespaces
#   E2E_SCOPE_WATCHED_CR         — CR name, identical in each namespace
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

WATCHED="${E2E_SCOPE_WATCHED_NAMESPACES:-e2e-scope-a,e2e-scope-b}"
CR="${E2E_SCOPE_WATCHED_CR:-e2e-scope-watched}"

IFS=',' read -r -a watched_list <<<"${WATCHED}"
for ns in "${watched_list[@]}"; do
  ns="${ns// /}"
  [[ -n "${ns}" ]] || continue

  log "Applying Neo4j CR ${CR} into watched namespace ${ns} (operator should reconcile it)"
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

log "CR ${CR} applied in every watched namespace (${WATCHED})"
