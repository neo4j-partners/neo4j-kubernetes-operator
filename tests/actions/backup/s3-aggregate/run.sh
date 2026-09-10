#!/usr/bin/env bash
# backup/s3-aggregate — prove object-store aggregate end to end against MinIO (ADR-016 increment):
#   1. write probe #1 into `neo4j`, take a Full Neo4jBackup to s3://<bucket>/<cr>/ (static-key creds).
#      Ad-hoc backups isolate into a daily chain, so it actually lands under s3://<bucket>/<cr>/<cr>-<UTCdate>/
#   2. write probe #2, take an Incremental Neo4jBackup to the SAME url — same UTC day, so it derives
#      the same daily chain and appends to the full under that prefix
#   3. take an Aggregate Neo4jBackup (source.backupRef=<inc>) — the aggregate Job resolves the chain
#      folder from the source's recorded uri and runs `neo4j-admin backup aggregate --from-path=<chain-folder> neo4j`
#      with NO mount, authenticating to MinIO via the projected AWS_* env, collapsing the full+inc
#      chain into a single recovered full written back under the same folder
#   4. write probe #post (after the aggregate, so NOT in the recovered full)
#   5. apply a Neo4jRestore(source.backupRef=<agg>, overwrite) over `neo4j`
# The assert (assert/restore-s3-aggregate) checks probe #1 AND #2 land (the aggregate captured the
# whole chain tip) and probe #post is gone. This is the object-store analogue of the PVC aggregate
# path — it validates the folder --from-path + db-operand call and the folder-seed on restore.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

FULL="${NEO4J_CR_NAME}-full"
INC="${NEO4J_CR_NAME}-inc"
AGG="${NEO4J_CR_NAME}-agg"
RESTORE_NAME="${NEO4J_CR_NAME}-run"
TARGET_DB="neo4j"
BUCKET="${S3_BUCKET:-neo4jbackups}"
SECRET="${S3_CREDS_SECRET:-e2e-s3-creds}"
S3_URL="s3://${BUCKET}/${NEO4J_CR_NAME}/"
POD="${NEO4J_STS_NAME}-0"
READY_TIMEOUT="${RESTORE_READY_TIMEOUT:-600s}"
BACKUP_TIMEOUT="${BACKUP_ASSERT_TIMEOUT:-600}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before the s3 aggregate round-trip (timeout ${READY_TIMEOUT})"
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

# wait_backup NAME — block until the named Neo4jBackup reaches Succeeded (or die with diagnostics).
wait_backup() {
  local name="$1" phase="" deadline=$((SECONDS + BACKUP_TIMEOUT))
  log "Waiting for Neo4jBackup ${name} to reach Succeeded (timeout ${BACKUP_TIMEOUT}s)"
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    phase="$(kubectl get "neo4jbackup/${name}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    [[ "${phase}" == "Succeeded" ]] && return 0
    [[ "${phase}" == "Failed" ]] && break
    sleep 5
  done
  kubectl describe "neo4jbackup/${name}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl logs -n "${NEO4J_NAMESPACE}" -l "job-name=${name}-backup" --tail=-1 >&2 || true
  kubectl logs -n "${NEO4J_NAMESPACE}" -l "job-name=${name}-aggregate" --tail=-1 >&2 || true
  die "Neo4jBackup ${name} did not reach Succeeded (last phase '${phase:-<none>}')"
}

log "Writing probe #1, then a Full Neo4jBackup ${FULL} → ${S3_URL}"
probe "e2e-s3-1"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: ${FULL}
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
wait_backup "${FULL}"

log "Writing probe #2, then an Incremental Neo4jBackup ${INC} → ${S3_URL} (same chain)"
probe "e2e-s3-2"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: ${INC}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["${TARGET_DB}"]
  type: Incremental
  destination:
    type: s3
    url: ${S3_URL}
    credentials:
      secretName: ${SECRET}
EOF
wait_backup "${INC}"

# Chain isolation (ADR-016): with no labels and no backupRef, both ad-hoc backups derive the SAME
# daily chain <cr>-<UTCdate> and nest under one prefix, so the incremental parents onto the full
# instead of mis-parenting. Derive the expected chain from what the operator recorded (not a locally
# computed date) so a run straddling UTC midnight can't false-fail.
full_chain="$(kubectl get "neo4jbackup/${FULL}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.chain}' 2>/dev/null || true)"
inc_chain="$(kubectl get "neo4jbackup/${INC}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.chain}' 2>/dev/null || true)"
[[ -n "${full_chain}" && "${full_chain}" == "${inc_chain}" ]] \
  || die "expected full and incremental to share one daily chain, got full='${full_chain}' inc='${inc_chain}'"
case "${full_chain}" in
  "${NEO4J_CR_NAME}-"*) : ;;
  *) die "daily chain '${full_chain}' should be shaped <cr>-<UTCdate>" ;;
esac
CHAIN_URL="${S3_URL}${full_chain}/"
full_uri="$(kubectl get "neo4jbackup/${FULL}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
inc_uri="$(kubectl get "neo4jbackup/${INC}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
[[ "${full_uri}" == "${CHAIN_URL}" && "${inc_uri}" == "${CHAIN_URL}" ]] \
  || die "expected full+inc artifacts nested under ${CHAIN_URL}, got full='${full_uri}' inc='${inc_uri}'"
log "Chain isolation OK — full+inc share daily chain ${full_chain}, nested under ${CHAIN_URL}"

log "Taking an Aggregate Neo4jBackup ${AGG} (source.backupRef=${INC}) → ${S3_URL}"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1beta1
kind: Neo4jBackup
metadata:
  name: ${AGG}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  databases: ["${TARGET_DB}"]
  type: Aggregate
  source:
    backupRef: ${INC}
  destination:
    type: s3
    url: ${S3_URL}
    credentials:
      secretName: ${SECRET}
EOF
wait_backup "${AGG}"

# The recovered full lands in the SAME dated chain folder the source chain lives in — proving the
# aggregate resolved the chain prefix from the source's recorded uri, not the flat base url.
agg_uri="$(kubectl get "neo4jbackup/${AGG}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.artifacts[0].uri}' 2>/dev/null || true)"
[[ "${agg_uri}" == "${CHAIN_URL}" ]] \
  || die "expected aggregate artifact under the chain folder ${CHAIN_URL}, got '${agg_uri}'"
log "Aggregate folder OK — recovered full recorded under ${CHAIN_URL}"

# probe #post lands after the aggregate — the overwrite restore must drop it.
log "Writing probe #post (after the aggregate, so NOT in the recovered full)"
probe "e2e-s3-post"

log "Applying Neo4jRestore ${RESTORE_NAME} (backupRef=${AGG}, overwrite ${TARGET_DB})"
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
    backupRef: ${AGG}
EOF
