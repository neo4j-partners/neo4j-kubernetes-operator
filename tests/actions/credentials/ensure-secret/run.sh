#!/usr/bin/env bash
# credentials/ensure-secret — pre-create the auth Secret referenced by passwordSecretRef.
# No-op unless AUTH_SECRET_CREATE=true (the generated-password case needs no secret).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

if [[ "${AUTH_SECRET_CREATE:-false}" != "true" ]]; then
  log "No pre-created auth Secret needed for this case"
  exit 0
fi

: "${AUTH_SECRET_NAME:?AUTH_SECRET_NAME required when AUTH_SECRET_CREATE=true}"
: "${AUTH_KNOWN_PASSWORD:?AUTH_KNOWN_PASSWORD required when AUTH_SECRET_CREATE=true}"

# Recreate so the known password is deterministic across reruns.
kubectl delete secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
  --ignore-not-found >/dev/null 2>&1 || true
kubectl create secret generic "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
  --from-literal=NEO4J_AUTH="neo4j/${AUTH_KNOWN_PASSWORD}" >/dev/null

log "Created auth Secret ${AUTH_SECRET_NAME} (user neo4j) in ${NEO4J_NAMESPACE}"
