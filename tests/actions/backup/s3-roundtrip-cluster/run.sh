#!/usr/bin/env bash
# backup/s3-roundtrip-cluster — cluster analogue of backup/s3-roundtrip (ADR-016 object store on a
# 3-primary cluster):
#   1. wait Ready (cluster formed), write probe #1 to `neo4j` over a ROUTED (neo4j://) session so
#      the write reaches the leader from whichever primary we exec into
#   2. take a Full Neo4jBackup to s3://<bucket>/<cr>/ with static-key destination.credentials
#   3. write probe #post (after the backup, so NOT in the artifact)
#   4. restore via source.backupRef with overwrite
# The restore issues CREATE OR REPLACE DATABASE neo4j OPTIONS {seedURI} TOPOLOGY 3 PRIMARIES
# (topology.defaultPrimariesCount=3), so every primary seeds from s3:// independently — the cluster
# restore contract. assert/restore-s3-cluster then checks the seed landed on all three members.
#
# Pods are addressed by the app.kubernetes.io/instance label, not <cr>-server-0: a Cluster renders
# one StatefulSet per pool (<cr>-primary), and the plain suite schema does not set NEO4J_POOL.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

BACKUP_NAME="${NEO4J_CR_NAME}-full"
RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
BUCKET="${S3_BUCKET:-neo4jbackups}"
SECRET="${S3_CREDS_SECRET:-e2e-s3-creds}"
S3_URL="s3://${BUCKET}/${NEO4J_CR_NAME}/"
READY_TIMEOUT="${CLUSTER_READY_TIMEOUT:-900s}" # cluster formation is slower than a single boot
BACKUP_TIMEOUT="${BACKUP_ASSERT_TIMEOUT:-600}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready (cluster formation, timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (cluster did not form)"
fi

# The primary pool StatefulSet is <cr>-primary; exec into member 0 directly. A label lookup on
# app.kubernetes.io/instance would ALSO match the completed backup Job pod (it shares CommonLabels),
# which sorts before the server pods and can't be exec'd ("cannot exec into a completed pod").
POD="${NEO4J_CR_NAME}-primary-0"
password="$(neo4j_password)"

# probe writes a row into `neo4j` over a ROUTED session (neo4j://): a direct bolt:// write to a
# non-leader primary is refused, so the driver must route to the leader.
probe() {
  local id="$1"
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a neo4j://localhost:7687 -d ${TARGET_DB} -u neo4j -p '${password}' --format plain \
     \"CREATE (:RestoreProbe {id:'${id}'});\"" \
    || die "failed to write probe ${id} into ${TARGET_DB}"
}

log "Writing probe #1, then taking a Full Neo4jBackup ${BACKUP_NAME} → ${S3_URL}"
probe "e2e-s3c-1"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: ${BACKUP_NAME}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["${TARGET_DB}"]
  type: Full
  destination:
    type: s3
    url: ${S3_URL}
    credentials:
      secretName: ${SECRET}
EOF

log "Waiting for Neo4jBackup ${BACKUP_NAME} to reach Succeeded (timeout ${BACKUP_TIMEOUT}s)"
deadline=$((SECONDS + BACKUP_TIMEOUT))
phase=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  phase="$(kubectl get "neo4jbackup/${BACKUP_NAME}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  [[ "${phase}" == "Succeeded" ]] && break
  [[ "${phase}" == "Failed" ]] && break
  sleep 5
done
if [[ "${phase}" != "Succeeded" ]]; then
  kubectl describe "neo4jbackup/${BACKUP_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl logs -n "${NEO4J_NAMESPACE}" -l "job-name=${BACKUP_NAME}-backup" --tail=-1 >&2 || true
  die "Neo4jBackup ${BACKUP_NAME} did not reach Succeeded (last phase '${phase:-<none>}')"
fi

log "Writing probe #post (after the backup, so NOT in the artifact)"
probe "e2e-s3c-post"

log "Applying Neo4jRestore ${RESTORE_NAME} (backupRef=${BACKUP_NAME}, overwrite ${TARGET_DB})"
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
  source:
    backupRef: ${BACKUP_NAME}
EOF
