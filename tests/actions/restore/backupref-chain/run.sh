#!/usr/bin/env bash
# restore/backupref-chain — prove the record-based restore path end to end over a backup CHAIN,
# on a PVC the workload also mounts (ADR-015 round-trip, BDR-014 §13):
#   1. write probe #1 into `neo4j`, take a Full  Neo4jBackup to the shared PVC   (<cr>-full)
#   2. write probe #2 into `neo4j`, take an Incremental Neo4jBackup to that PVC  (<cr>-inc)
#   3. apply a Neo4jRestore with source.backupRef=<cr>-inc → seed a fresh db `restored`
# The operator resolves the incremental's recorded pvc:// artifact to file:/backups/<path>; Neo4j's
# FileSeedProvider then walks the whole chain (full → incremental) from that directory. The assert
# checks BOTH probes land in `restored` — proving the incremental's data is carried, not just the
# full's (the gap raised during manual testing).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

FULL_BACKUP="${NEO4J_CR_NAME}-full"
INC_BACKUP="${NEO4J_CR_NAME}-inc"
RESTORE_NAME="${NEO4J_CR_NAME}-run"
RESTORED_DB="restored"
CLAIM="e2e-backupref-dest"
POD="${NEO4J_STS_NAME}-0"
READY_TIMEOUT="${RESTORE_READY_TIMEOUT:-600s}"
BACKUP_TIMEOUT="${BACKUP_ASSERT_TIMEOUT:-600}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before the round-trip (timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (Enterprise image pull may be required)"
fi

password="$(neo4j_password)"

# cypher writes a probe row into `neo4j` over bolt.
probe() {
  local id="$1"
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d neo4j -u neo4j -p '${password}' --format plain \
     \"CREATE (:RestoreProbe {id:'${id}'});\"" \
    || die "failed to write probe ${id} into neo4j"
}

# apply a Neo4jBackup of the given type and wait for it to reach Succeeded.
run_backup() {
  local name="$1" type="$2"
  log "Applying ${type} Neo4jBackup ${name} → pvc ${CLAIM}"
  kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: ${name}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["neo4j"]
  type: ${type}
  destination:
    type: pvc
    pvc:
      claimName: ${CLAIM}
EOF
  log "Waiting for Neo4jBackup ${name} to reach Succeeded (timeout ${BACKUP_TIMEOUT}s)"
  local deadline=$((SECONDS + BACKUP_TIMEOUT)) phase=""
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    phase="$(kubectl get "neo4jbackup/${name}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    [[ "${phase}" == "Succeeded" ]] && return 0
    [[ "${phase}" == "Failed" ]] && break
    sleep 5
  done
  kubectl describe "neo4jbackup/${name}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl logs -n "${NEO4J_NAMESPACE}" -l "job-name=${name}-backup" --tail=-1 >&2 || true
  die "Neo4jBackup ${name} did not reach Succeeded (last phase '${phase:-<none>}')"
}

probe "e2e-bref-1"
run_backup "${FULL_BACKUP}" Full

probe "e2e-bref-2"
run_backup "${INC_BACKUP}" Incremental

log "Applying Neo4jRestore ${RESTORE_NAME} (backupRef=${INC_BACKUP} → seed ${RESTORED_DB})"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: ${RESTORE_NAME}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["${RESTORED_DB}"]
  source:
    backupRef: ${INC_BACKUP}
EOF
