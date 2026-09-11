#!/usr/bin/env bash
# backup/s3-roundtrip-cluster verify — the Full Neo4jBackup reached Succeeded and recorded an s3://
# artifact, proving a cluster member streamed the backup to object storage. The restore + per-member
# seed are asserted separately by assert/restore-s3-cluster.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

BACKUP_NAME="${NEO4J_CR_NAME}-full"
RES="neo4jbackup/${BACKUP_NAME}"
EXPECT_REASON="${BACKUP_EXPECT_REASON:-BackupSucceeded}"
oracle_require BackupReady "${EXPECT_REASON}"

phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="BackupReady")].reason}' 2>/dev/null || true)"
[[ "${phase}" == "Succeeded" && "${reason}" == "${EXPECT_REASON}" ]] \
  || die "expected ${RES} phase=Succeeded BackupReady=${EXPECT_REASON}, got phase='${phase:-<none>}' reason='${reason:-<none>}'"

uri="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
[[ "${uri}" == s3://* ]] \
  || die "expected an s3:// artifact URI on ${RES}, got '${uri:-<none>}'"

log "Neo4jBackup ${BACKUP_NAME} Succeeded with object-store artifact ${uri}"
