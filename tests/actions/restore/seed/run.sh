#!/usr/bin/env bash
# restore/seed — prove Neo4jRestore end to end with a credential-free file: seed (ADR-015).
# Once the target is Ready we: (1) write a probe row into `neo4j`, (2) produce a backup artifact
# in the pod's own /tmp via `neo4j-admin database backup` (no external store, no cloud identity),
# (3) apply a Neo4jRestore that seeds a FRESH database `restored` from that file: URI. The restore
# runs over Bolt (CREATE DATABASE … OPTIONS {seedURI}); assert/restore-succeeded checks the copy.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
RESTORED_DB="restored"
PROBE_ID="e2e-restore"
POD="${NEO4J_STS_NAME}-0"
READY_TIMEOUT="${RESTORE_READY_TIMEOUT:-600s}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before seeding (timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (Enterprise image pull may be required)"
fi

password="$(neo4j_password)"

log "Writing probe (:RestoreProbe {id:'${PROBE_ID}'}) into database neo4j"
kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a bolt://localhost:7687 -d neo4j -u neo4j -p '${password}' --format plain \
   \"CREATE (:RestoreProbe {id:'${PROBE_ID}'});\"" \
  || die "failed to write probe row into neo4j"

log "Producing a backup artifact in ${POD}:/tmp via neo4j-admin database backup"
kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "neo4j-admin database backup --to-path=/tmp --from=localhost:6362 neo4j" \
  || die "neo4j-admin database backup failed"

# neo4j-admin writes <database>-<timestamp>.backup; the FileSeedProvider seeds from that file.
artifact="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "ls -1t /tmp/neo4j*.backup 2>/dev/null | head -1" | tr -d '\r')"
[[ -n "${artifact}" ]] || die "no /tmp/neo4j*.backup artifact found after backup"
log "Seed artifact: ${artifact}"

log "Applying Neo4jRestore ${RESTORE_NAME} (seed ${RESTORED_DB} from file:${artifact})"
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
    url: "file:${artifact}"
EOF
