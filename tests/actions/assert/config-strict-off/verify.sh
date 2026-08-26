#!/usr/bin/env bash
# assert/config-strict-off — NEO-3-003-CFG-02: the strict config validation toggle actually
# toggles. This is the mirror of assert/config-startup-error (AC-NEO-CONFIG-002), which proves
# that with strict validation ON an unknown setting is a fatal startup error. Here the same
# unknown setting is present but strict validation is OFF, so Neo4j downgrades it to a warning:
# the workload becomes Ready (proving the unknown key was tolerated) and SHOW SETTINGS reports
# server.config.strict_validation.enabled=false (proving the toggle is in effect at runtime).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

SETTING_NAME="server.config.strict_validation.enabled"
SETTING_VALUE="false"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"

# Ready DESPITE the unknown key in the fixture is the core proof: with strict validation on the
# workload never reaches Ready (see assert/config-startup-error), so becoming Ready here can only
# mean strict-off downgraded the unknown setting to a warning.
log "Waiting for ${NEO4J_RESOURCE} Ready — strict validation off must tolerate the unknown setting"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=600s >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready — strict validation off did not tolerate the unknown setting"
fi

# Run cypher-shell inside the Neo4j container over its localhost bolt interface.
conn_exec_serverpod() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"
}
CONN_EXEC_FN=conn_exec_serverpod

password="$(neo4j_password)"
conn_assert_setting localhost "${password}" "${SETTING_NAME}" "${SETTING_VALUE}" "config-strict-off"

log "strict validation off tolerated the unknown setting and ${SETTING_NAME}=${SETTING_VALUE} at runtime (NEO-3-003-CFG-02)"
