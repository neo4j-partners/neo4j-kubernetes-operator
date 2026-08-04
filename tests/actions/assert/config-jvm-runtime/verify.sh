#!/usr/bin/env bash
# assert/config-jvm-runtime — NEO-3-003-JVM-02 (runtime): spec.config.jvm.additionalArguments
# are not just rendered into server.jvm.additional, they take effect on the running server.
# Reproduces a manual check: connect over bolt with cypher-shell, SHOW SETTINGS the
# server.jvm.additional setting, and confirm the effective value contains the custom flag.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

# Setting/flag expected at runtime — tied to tests/fixtures/neo4j-config-jvm.yaml.
SETTING_NAME="${EXPECT_SETTING_NAME:-server.jvm.additional}"
EXPECT_JVM_ARG="${EXPECT_JVM_ARG:--XX:+ExitOnOutOfMemoryError}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"

log "Waiting for ${NEO4J_RESOURCE} Ready (Neo4j must accept connections for SHOW SETTINGS)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready"
fi

# Run cypher-shell inside the Neo4j container over its localhost bolt interface.
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

password="$(neo4j_password)"
conn_assert_setting localhost "${password}" "${SETTING_NAME}" "${EXPECT_JVM_ARG}" "config-jvm-runtime"

log "spec.config.jvm.additionalArguments effective at runtime in ${SETTING_NAME} ('${EXPECT_JVM_ARG}') (NEO-3-003-JVM-02)"
