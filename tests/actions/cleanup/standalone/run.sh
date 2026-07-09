#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --wait=false || true
kubectl delete pvc -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" --ignore-not-found || true

log "Neo4j workload cleanup done"
