#!/usr/bin/env bash
# storage/cleanup-extra — remove storage prerequisites that are NOT owned by the CR and
# thus survive cleanup/standalone: the pre-created PVC referenced by existing.claimName.
# Best-effort and idempotent (no-op when the case created no such PVC).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete pvc -n "${NEO4J_NAMESPACE}" \
  -l "app.kubernetes.io/managed-by=neo4j-e2e" --ignore-not-found --wait=false >/dev/null 2>&1 || true

log "Removed pre-created storage PVCs (managed-by=neo4j-e2e), if any"
