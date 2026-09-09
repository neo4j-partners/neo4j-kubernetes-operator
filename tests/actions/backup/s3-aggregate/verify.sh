#!/usr/bin/env bash
# backup/s3-aggregate verify — the Aggregate Neo4jBackup reached Succeeded and recorded an s3://
# FOLDER artifact with an EMPTY path (unlike PVC aggregate, object-store aggregate records the url,
# not a recovered filename — restore seeds the folder). This proves the aggregate Job authenticated
# to MinIO and `neo4j-admin backup aggregate --from-path=<url>` ran without a mount. The restore side
# is asserted separately by assert/restore-s3-aggregate.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

AGG="${NEO4J_CR_NAME}-agg"
RES="neo4jbackup/${AGG}"
EXPECT_REASON="${BACKUP_EXPECT_REASON:-BackupSucceeded}"
oracle_require BackupReady "${EXPECT_REASON}"

phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="BackupReady")].reason}' 2>/dev/null || true)"
[[ "${phase}" == "Succeeded" && "${reason}" == "${EXPECT_REASON}" ]] \
  || die "expected ${RES} phase=Succeeded BackupReady=${EXPECT_REASON}, got phase='${phase:-<none>}' reason='${reason:-<none>}'"

uri="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
[[ "${uri}" == s3://* ]] \
  || die "expected an s3:// aggregate artifact URI on ${RES}, got '${uri:-<none>}'"

path="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].path}' 2>/dev/null || true)"
[[ -z "${path}" ]] \
  || die "expected an EMPTY artifact path for object-store aggregate (restore seeds the folder ${uri}), got '${path}'"

typ="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].type}' 2>/dev/null || true)"
[[ "${typ}" == "Full" ]] \
  || die "expected the recovered aggregate artifact to be type Full (a standalone full), got '${typ:-<none>}'"

log "Neo4jBackup ${AGG} Succeeded — object-store aggregate recorded folder artifact ${uri} (type Full, empty path)"
