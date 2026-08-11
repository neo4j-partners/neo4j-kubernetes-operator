#!/usr/bin/env bash
# credentials/ensure-secret — pre-create the auth Secret referenced by passwordSecretRef,
# carrying the opt-in labels the operator requires (NEO-005, ADD-01).
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
: "${NEO4J_CR_NAME:?NEO4J_CR_NAME required to delegate the auth Secret (ADD-01)}"

# Recreate so the known password is deterministic across reruns.
kubectl delete secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
  --ignore-not-found >/dev/null 2>&1 || true
kubectl create secret generic "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
  --from-literal=NEO4J_AUTH="neo4j/${AUTH_KNOWN_PASSWORD}" >/dev/null

# Without both labels the operator refuses the Secret and never renders operands: the namespace
# owner must opt in (NEO-005) and a BYO auth Secret must be delegated to one CR name (ADD-01).
# AUTH_SECRET_LABELS drops one or both so negative cases can assert each refusal.
LABEL_MODE="${AUTH_SECRET_LABELS:-full}"
case "${LABEL_MODE}" in
  full)      labels=("neo4j.com/mountable-by-operator=true" "neo4j.com/allowed-for=${NEO4J_CR_NAME}") ;;
  mountable) labels=("neo4j.com/mountable-by-operator=true") ;;
  none)      labels=() ;;
  *) die "unknown AUTH_SECRET_LABELS='${LABEL_MODE}' (expected full|mountable|none)" ;;
esac

if [[ "${#labels[@]}" -gt 0 ]]; then
  kubectl label secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" "${labels[@]}" >/dev/null
fi

log "Created auth Secret ${AUTH_SECRET_NAME} (user neo4j) in ${NEO4J_NAMESPACE} (labels=${LABEL_MODE})"
