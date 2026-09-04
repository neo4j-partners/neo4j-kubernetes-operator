#!/usr/bin/env bash
# restore/backupref-cleanup — remove the two chain backups (their owned Jobs are GC'd via ownerRef)
# and the shared destination PVC. Pairs with restore/cleanup (which clears the Neo4jRestore and the
# seeded `restored` db) so a re-run starts clean. cleanup/standalone (pipeline teardown) removes the
# Neo4j CR + owned data PVC afterwards. Best-effort (case_teardown swallows failures).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

FULL_BACKUP="${NEO4J_CR_NAME}-full"
INC_BACKUP="${NEO4J_CR_NAME}-inc"
CLAIM="e2e-backupref-dest"

kubectl delete neo4jbackup "${INC_BACKUP}" "${FULL_BACKUP}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete job "${INC_BACKUP}-backup" "${FULL_BACKUP}-backup" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete pvc "${CLAIM}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s \
  || log "WARNING: destination PVC ${CLAIM} still terminating"

log "backupRef chain cleanup done"
