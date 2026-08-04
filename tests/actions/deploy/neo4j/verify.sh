#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

if [[ "${CASE_EXPECT:-success}" == "failure" ]]; then
  log "Skipping CR presence check (expected admission failure)"
  exit 0
fi

kubectl get neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" >/dev/null \
  || die "${NEO4J_KIND} CR ${NEO4J_CR_NAME} not found"

log "${NEO4J_KIND} CR applied"
