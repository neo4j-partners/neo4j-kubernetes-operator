#!/usr/bin/env bash
# backup/s3-roundtrip — prove the ADR-016 object-store credential path end to end against MinIO:
#   1. write probe #1 into `neo4j`, take a Full Neo4jBackup to s3://<bucket>/<cr>/ with static-key
#      destination.credentials (the backup Job authenticates to MinIO via the projected AWS_* env)
#   2. write probe #post into `neo4j` — this lands AFTER the backup, so it is NOT in the artifact
#   3. apply a Neo4jRestore(source.backupRef=<full>, overwrite) over `neo4j`
# The operator records the s3:// artifact URI, and on restore the server pods (which carry the same
# static keys via spec.security.cloudIdentity.staticKeySecret, and have CloudSeedProvider enabled)
# seed-from-URI straight from MinIO. The assert (assert/restore-s3) checks probe #1 lands and probe
# #post is gone (overwrite replaced the live store from the backup, not merged into it).
#
# This is the object-store analogue of restore/backupref-chain (which uses a PVC + FileSeedProvider);
# it deliberately skips the incremental chain and restoreMetadata (metadata apply needs a PVC-backed
# artifact on the server filesystem) to keep the focus on the credential + CloudSeedProvider path.
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
POD="${NEO4J_STS_NAME}-0"
READY_TIMEOUT="${RESTORE_READY_TIMEOUT:-600s}"
BACKUP_TIMEOUT="${BACKUP_ASSERT_TIMEOUT:-600}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before the s3 round-trip (timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (Enterprise image pull may be required)"
fi

password="$(neo4j_password)"

# probe writes a probe row into `neo4j` over bolt.
probe() {
  local id="$1"
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d ${TARGET_DB} -u neo4j -p '${password}' --format plain \
     \"CREATE (:RestoreProbe {id:'${id}'});\"" \
    || die "failed to write probe ${id} into ${TARGET_DB}"
}

log "Writing probe #1, then taking a Full Neo4jBackup ${BACKUP_NAME} → ${S3_URL}"
probe "e2e-s3-1"
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

# probe #post lands after the backup — the overwrite restore must drop it.
log "Writing probe #post (after the backup, so NOT in the artifact)"
probe "e2e-s3-post"

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
