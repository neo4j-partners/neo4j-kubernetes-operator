#!/usr/bin/env bash
# rotation/apply — stage a new admin credential and file a Neo4jPasswordRotation (BDR-017 / ADR-019).
# Once the target is Ready we: (1) capture the current (OLD) password from <cr>-auth and stash it in a
# plain Secret so assert/rotation-succeeded can prove it was invalidated, (2) create the staged Secret
# holding a NEW deterministic password, delegated to this instance, (3) apply a Neo4jPasswordRotation.
# The operator authenticates with the still-live old value, runs ALTER USER neo4j SET PASSWORD over the
# system database, then writes the new value into <cr>-auth.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

ROTATION_NAME="${NEO4J_CR_NAME}-run"
STAGED_SECRET="${NEO4J_CR_NAME}-new-credential"
OLDPW_SECRET="${NEO4J_CR_NAME}-e2e-oldpw"
# Deterministic so assert/rotation-succeeded can recompute it without sharing state; satisfies the
# NEO4J_AUTH grammar (user neo4j, no leading '-', no '/').
NEW_PASSWORD="Rotated-${NEO4J_CR_NAME}-pw1"
READY_TIMEOUT="${ROTATION_READY_TIMEOUT:-600s}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before rotating (timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (Enterprise image pull may be required)"
fi

old_password="$(neo4j_password)"
log "Stashing the current password in ${OLDPW_SECRET} so the assert can prove it is invalidated"
kubectl create secret generic "${OLDPW_SECRET}" -n "${NEO4J_NAMESPACE}" \
  --from-literal="password=${old_password}" --dry-run=client -o yaml | kubectl apply -f -

log "Staging the new credential in ${STAGED_SECRET} (delegated to ${NEO4J_CR_NAME})"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: ${STAGED_SECRET}
  labels:
    neo4j.com/mountable-by-operator: "true"
    neo4j.com/allowed-for: ${NEO4J_CR_NAME}
stringData:
  NEO4J_AUTH: "neo4j/${NEW_PASSWORD}"
EOF

log "Applying Neo4jPasswordRotation ${ROTATION_NAME}"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: neo4j.com/v1
kind: Neo4jPasswordRotation
metadata:
  name: ${ROTATION_NAME}
spec:
  neo4jRef:
    name: ${NEO4J_CR_NAME}
  newPasswordSecretRef:
    name: ${STAGED_SECRET}
EOF
