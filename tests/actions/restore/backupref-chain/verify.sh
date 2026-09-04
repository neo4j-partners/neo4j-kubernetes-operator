#!/usr/bin/env bash
# restore/backupref-chain verify — the round-trip's inputs landed: both chain backups reached
# Succeeded and the Neo4jRestore record was accepted. The seed outcome (online + both probes) is
# asserted by assert/restore-chain.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

FULL_BACKUP="${NEO4J_CR_NAME}-full"
INC_BACKUP="${NEO4J_CR_NAME}-inc"
RESTORE_NAME="${NEO4J_CR_NAME}-run"

for b in "${FULL_BACKUP}" "${INC_BACKUP}"; do
  phase="$(kubectl get "neo4jbackup/${b}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  [[ "${phase}" == "Succeeded" ]] \
    || die "Neo4jBackup ${b} expected Succeeded before restore, got '${phase:-<none>}'"
done

kubectl get neo4jrestore "${RESTORE_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "Neo4jRestore ${RESTORE_NAME} not found after apply"
log "chain backups Succeeded (${FULL_BACKUP}, ${INC_BACKUP}); Neo4jRestore ${RESTORE_NAME} created"
