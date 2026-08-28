#!/usr/bin/env bash
# backup/cleanup — remove the Neo4jBackup (its owned Job is GC'd via ownerRef) and the
# non-owned destination PVC. cleanup/standalone already removed the Neo4j CR + owned data PVC;
# this mirrors storage/cleanup-extra for the pre-created destination claim. Best-effort
# (case_teardown swallows failures), never fails the suite.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

BACKUP_NAME="${NEO4J_CR_NAME}-run"

kubectl delete neo4jbackup "${BACKUP_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete job "${BACKUP_NAME}-backup" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete pvc e2e-backup-dest -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s \
  || log "WARNING: destination PVC e2e-backup-dest still terminating"

log "Neo4jBackup cleanup done"
