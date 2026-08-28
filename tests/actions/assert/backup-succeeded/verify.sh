#!/usr/bin/env bash
# assert/backup-succeeded — the Neo4jBackup completes end to end:
#   - the owned run-to-completion Job (<cr>-run-backup) reaches condition=complete
#   - the Neo4jBackup reaches status.phase=Succeeded with BackupReady=True/BackupSucceeded
#   - status.artifacts records the destination and status.chain is set
# Contract sources: src/internal/controller/neo4jbackup/reconciler.go, render/backup/job.go,
# reasons via tests/lib/oracle.sh (BackupReady/BackupSucceeded).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

BACKUP_NAME="${NEO4J_CR_NAME}-run"
JOB_NAME="${BACKUP_NAME}-backup"
RES="neo4jbackup/${BACKUP_NAME}"
TIMEOUT="${BACKUP_ASSERT_TIMEOUT:-600}"
EXPECT_REASON="${BACKUP_EXPECT_REASON:-BackupSucceeded}"
oracle_require BackupReady "${EXPECT_REASON}"

kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "${RES} not found — did backup/create apply the Neo4jBackup?"

log "Waiting for Job ${JOB_NAME} to complete (timeout ${TIMEOUT}s)"
if ! kubectl wait --for=condition=complete "job/${JOB_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}s" 2>/dev/null; then
  kubectl describe "job/${JOB_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl logs -n "${NEO4J_NAMESPACE}" -l "job-name=${JOB_NAME}" --tail=-1 >&2 || true
  kubectl describe "${RES}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "backup Job ${JOB_NAME} did not complete within ${TIMEOUT}s"
fi

phase="" status="" reason="" chain="" arturi=""
deadline=$((SECONDS + 120))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  status="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="BackupReady")].status}' 2>/dev/null || true)"
  reason="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="BackupReady")].reason}' 2>/dev/null || true)"
  [[ "${phase}" == "Succeeded" && "${status}" == "True" && "${reason}" == "${EXPECT_REASON}" ]] && break
  sleep 5
done

[[ "${phase}" == "Succeeded" ]] \
  || die "expected ${RES} phase=Succeeded, got '${phase:-<none>}'"
[[ "${status}" == "True" && "${reason}" == "${EXPECT_REASON}" ]] \
  || die "expected BackupReady=True/${EXPECT_REASON}, got status='${status:-<none>}' reason='${reason:-<none>}'"

chain="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.chain}' 2>/dev/null || true)"
[[ -n "${chain}" ]] || die "expected ${RES} status.chain to be set"
arturi="$(kubectl get "${RES}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
[[ "${arturi}" == "pvc://e2e-backup-dest" ]] \
  || die "expected status.artifacts[0].uri=pvc://e2e-backup-dest, got '${arturi:-<none>}'"

log "Neo4jBackup ${BACKUP_NAME} Succeeded; Job ${JOB_NAME} complete; chain=${chain}, artifact=${arturi}"
