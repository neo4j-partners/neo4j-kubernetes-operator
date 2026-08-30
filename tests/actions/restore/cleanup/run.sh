#!/usr/bin/env bash
# restore/cleanup — remove the Neo4jRestore record and the seeded `restored` database.
# cleanup/standalone removes the Neo4j CR + owned data PVC; this only clears the restore
# artifacts so a re-run starts clean. Best-effort (case_teardown swallows failures).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

RESTORE_NAME="${NEO4J_CR_NAME}-run"
RESTORED_DB="restored"
POD="${NEO4J_STS_NAME}-0"

kubectl delete neo4jrestore "${RESTORE_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true

# Drop the seeded database if the workload is still up (before cleanup/standalone tears it down).
if password="$(neo4j_password 2>/dev/null)"; then
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' \
     \"DROP DATABASE ${RESTORED_DB} IF EXISTS;\"" 2>/dev/null || true
fi

log "Neo4jRestore cleanup done"
