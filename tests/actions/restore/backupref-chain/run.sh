#!/usr/bin/env bash
# restore/backupref-chain — prove the record-based, overwrite restore path over a backup CHAIN,
# on a PVC the workload also mounts (ADR-015 round-trip, BDR-014 §13):
#   1. write probe #1 into `neo4j`, take a Full  Neo4jBackup to the shared PVC   (<cr>-full)
#   2. write probe #2 into `neo4j`, take an Incremental Neo4jBackup to that PVC  (<cr>-inc)
#   3. write probe #3 into `neo4j` — this lands AFTER the last backup, so it is NOT in the chain
#   4. DROP the role so the target no longer has it, then apply a
#      Neo4jRestore(source.backupRef=<cr>-inc, overwrite+forceOffline, restoreMetadata) over `neo4j`
# backupRef resolves the artifact by database name, so we restore the same database (`neo4j`),
# overwriting the live store. The operator resolves the incremental's pvc:// artifact to
# file:/backups/<path> and Neo4j's FileSeedProvider walks the full → incremental chain. It also runs
# the post-seed metadata Job (spec.restoreMetadata) to reapply users/roles/privileges. The assert
# checks probes #1 and #2 land AND probe #3 is gone (chain + overwrite), AND the dropped role is
# back (metadata apply). A role granted on `neo4j` before the backup exercises this: it is captured
# in the backup, dropped before restore, and recreated by the metadata script.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

FULL_BACKUP="${NEO4J_CR_NAME}-full"
INC_BACKUP="${NEO4J_CR_NAME}-inc"
RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
META_ROLE="e2ebrefrole"
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
    "cypher-shell -a bolt://localhost:7687 -d ${TARGET_DB} -u neo4j -p '${password}' --format plain \
     \"CREATE (:RestoreProbe {id:'${id}'});\"" \
    || die "failed to write probe ${id} into ${TARGET_DB}"
}

# sys runs a statement against the system database (roles/users live there).
sys() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \"$1\"" \
    || die "system statement failed: $1"
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
  databases: ["${TARGET_DB}"]
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

# A role granted on `neo4j` is captured in the backup's metadata; it is dropped before the restore
# so the metadata Job has to recreate it (a clean CREATE, no conflict).
log "Creating role ${META_ROLE} (granted ACCESS on ${TARGET_DB}) before the backup"
sys "CREATE ROLE ${META_ROLE};"
sys "GRANT ACCESS ON DATABASE ${TARGET_DB} TO ${META_ROLE};"

probe "e2e-bref-1"
run_backup "${FULL_BACKUP}" Full

probe "e2e-bref-2"
run_backup "${INC_BACKUP}" Incremental

# probe #3 lands after the last backup — the overwrite restore must drop it.
probe "e2e-bref-post"

# Drop the role so the target no longer has it; a successful restore must bring it back.
log "Dropping role ${META_ROLE} so the metadata apply has to recreate it"
sys "DROP ROLE ${META_ROLE};"

log "Applying Neo4jRestore ${RESTORE_NAME} (backupRef=${INC_BACKUP}, overwrite+metadata ${TARGET_DB})"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jRestore
metadata:
  name: ${RESTORE_NAME}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["${TARGET_DB}"]
  overwrite: true
  forceOffline: true
  restoreMetadata: true
  source:
    backupRef: ${INC_BACKUP}
EOF
