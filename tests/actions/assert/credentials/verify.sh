#!/usr/bin/env bash
# assert/credentials — verify the bootstrap password (generated or referenced) and that
# it actually authenticates a bolt query from the Neo4j pod.
#   - generated:  Secret <cr>-auth created by the operator, status.credentials.generated=true
#   - secret-ref: pre-created Secret used as-is,             status.credentials.generated=false
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
EXPECT_GENERATED="${CRED_EXPECT_GENERATED:-true}"

log "Waiting for ${NEO4J_RESOURCE} Ready (Neo4j must accept connections)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready"
fi

# --- status.credentials ------------------------------------------------------
gen="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.credentials.generated}' 2>/dev/null || true)"
secret="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.credentials.secretName}' 2>/dev/null || true)"

if [[ "${EXPECT_GENERATED}" == "true" ]]; then
  # 'generated' is omitempty — true is serialised, false is omitted.
  [[ "${gen}" == "true" ]] \
    || die "status.credentials.generated=${gen:-<absent>}, expected true"
else
  [[ -z "${gen}" || "${gen}" == "false" ]] \
    || die "status.credentials.generated=${gen}, expected false"
fi

if [[ -n "${CRED_EXPECT_SECRET:-}" ]]; then
  [[ "${secret}" == "${CRED_EXPECT_SECRET}" ]] \
    || die "status.credentials.secretName='${secret}', expected '${CRED_EXPECT_SECRET}'"
fi
log "status.credentials OK (generated=${gen:-false}, secretName=${secret})"

# --- resolve the expected password ------------------------------------------
if [[ "${EXPECT_GENERATED}" == "true" ]]; then
  # Read the operator-generated password from <cr>-auth (NEO4J_AUTH_SECRET).
  password="$(neo4j_password)"
  log "Using operator-generated password from ${NEO4J_AUTH_SECRET}"
else
  : "${AUTH_KNOWN_PASSWORD:?AUTH_KNOWN_PASSWORD required for secret-ref case}"
  password="${AUTH_KNOWN_PASSWORD}"
  log "Using pre-created password from ${AUTH_SECRET_NAME}"
fi

# --- prove the password authenticates a bolt query --------------------------
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

conn_assert_one bolt success localhost "${password}" "credentials"
# A wrong password must be rejected — proves auth is actually enforced with this password.
conn_assert_one bolt failure localhost "${password}-wrong" "credentials-negative"

log "Bolt authentication verified with the expected password (${NEO4J_RESOURCE})"
