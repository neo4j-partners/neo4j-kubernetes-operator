#!/usr/bin/env bash
# backup/s3-aggregate-cleanup — remove the aggregate suite's Neo4jBackup (full/inc/agg) + Neo4jRestore
# records and their owned Jobs so a re-run starts clean. The s3 artifacts live in MinIO's emptyDir and
# are discarded by minio/cleanup; cleanup/standalone removes the Neo4j CR + data PVC. Best-effort
# (case_teardown swallows failures).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
kubectl delete neo4jrestore "${RESTORE_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
for b in full inc agg; do
  name="${NEO4J_CR_NAME}-${b}"
  kubectl delete neo4jbackup "${name}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
  kubectl delete job "${name}-backup" "${name}-aggregate" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
done

log "s3 aggregate backup/restore cleanup done"
