#!/usr/bin/env bash
# backup/s3-cleanup — remove the Neo4jBackup + Neo4jRestore records and the backup's owned Job so a
# re-run starts clean. The s3 artifact lives in MinIO's emptyDir and is discarded when minio/cleanup
# tears the pod down. cleanup/standalone (pipeline teardown) removes the Neo4j CR + data PVC.
# Best-effort (case_teardown swallows failures).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

BACKUP_NAME="${NEO4J_CR_NAME}-full"
RESTORE_NAME="${NEO4J_CR_NAME}-run"

kubectl delete neo4jrestore "${RESTORE_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete neo4jbackup "${BACKUP_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete job "${BACKUP_NAME}-backup" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true

log "s3 backup/restore cleanup done"
