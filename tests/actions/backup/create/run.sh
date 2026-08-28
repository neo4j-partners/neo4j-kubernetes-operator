#!/usr/bin/env bash
# backup/create — apply the Neo4jBackup once the target Neo4j is Ready.
# The backup Job dials the target's backup listener, so creating the record before the workload
# serves would let the Job's backoffLimit exhaust against a not-yet-listening admin endpoint.
# Waiting for Ready here (not just Installed) makes the suite robust regardless of
# E2E_ASSERT_NEO4J_READY.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

BACKUP_MANIFEST="${REPO_ROOT}/tests/fixtures/neo4jbackup-pvc.yaml"
READY_TIMEOUT="${BACKUP_READY_TIMEOUT:-600s}"

log "Waiting for neo4j/${NEO4J_CR_NAME} Ready before creating the backup (timeout ${READY_TIMEOUT})"
if ! kubectl wait --for=condition=Ready "neo4j/${NEO4J_CR_NAME}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${READY_TIMEOUT}" 2>/dev/null; then
  kubectl describe "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "neo4j/${NEO4J_CR_NAME} not Ready within ${READY_TIMEOUT} (Enterprise image pull may be required)"
fi

rendered="$(mktemp)"
sed "s|__CR_NAME__|${NEO4J_CR_NAME}|g" "${BACKUP_MANIFEST}" >"${rendered}"
log "Applying Neo4jBackup ${NEO4J_CR_NAME}-run in namespace ${NEO4J_NAMESPACE}"
kubectl apply -n "${NEO4J_NAMESPACE}" -f "${rendered}"
rm -f "${rendered}"
