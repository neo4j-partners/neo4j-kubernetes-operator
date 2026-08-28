#!/usr/bin/env bash
# backup/create verify — the Neo4jBackup record was accepted by the API server.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl get neo4jbackup "${NEO4J_CR_NAME}-run" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "Neo4jBackup ${NEO4J_CR_NAME}-run not found after apply"
log "Neo4jBackup ${NEO4J_CR_NAME}-run created"
