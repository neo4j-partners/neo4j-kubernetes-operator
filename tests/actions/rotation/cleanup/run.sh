#!/usr/bin/env bash
# rotation/cleanup — remove the Neo4jPasswordRotation record and the e2e-only Secrets.
# cleanup/standalone removes the Neo4j CR + owned data PVC; this only clears rotation artifacts so a
# re-run starts clean. Best-effort (case_teardown swallows failures).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

ROTATION_NAME="${NEO4J_CR_NAME}-run"
STAGED_SECRET="${NEO4J_CR_NAME}-new-credential"
OLDPW_SECRET="${NEO4J_CR_NAME}-e2e-oldpw"

kubectl delete neo4jpasswordrotation "${ROTATION_NAME}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete secret "${STAGED_SECRET}" "${OLDPW_SECRET}" -n "${NEO4J_NAMESPACE}" --ignore-not-found || true

log "Neo4jPasswordRotation cleanup done"
