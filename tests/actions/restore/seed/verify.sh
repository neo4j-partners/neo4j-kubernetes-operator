#!/usr/bin/env bash
# restore/seed verify — the Neo4jRestore record was accepted by the API server.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl get neo4jrestore "${NEO4J_CR_NAME}-run" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "Neo4jRestore ${NEO4J_CR_NAME}-run not found after apply"
log "Neo4jRestore ${NEO4J_CR_NAME}-run created"
