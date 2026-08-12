#!/usr/bin/env bash
# assert/config-jvm-defaults — NEO-3-003-JVM-01: with spec.config.jvm.useDefaults true the
# operator injects Neo4j's default JVM arguments into server.jvm.additional, emits them before
# the user's additionalArguments (so a user flag on the same key overrides in place), and the
# merged list is what the running server reports.
#
# The rendered ConfigMap is checked first because it is the discriminating half: the image
# could ship its own defaults, only the ConfigMap proves the operator injected them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

# Standalone pool ConfigMap; override via NEO4J_CONFIGMAP for other pools.
CONFIGMAP="${NEO4J_CONFIGMAP:-${NEO4J_CR_NAME}-config}"
SETTING_NAME="${EXPECT_SETTING_NAME:-server.jvm.additional}"
# Sample of neo4jDefaultJVMAdditional (src/internal/render/serverconfig/configmap.go), one per
# flag shape so a regression in normalisation or quoting shows up.
EXPECT_DEFAULT_ARGS=(
  "-XX:+UseG1GC"
  "-XX:-OmitStackTraceInFastThrow"
  "-Djdk.tls.ephemeralDHKeySize=2048"
  "--add-opens=java.base/java.nio=ALL-UNNAMED"
)
# Tied to tests/fixtures/neo4j-config-jvm-defaults.yaml.
EXPECT_JVM_ARG="${EXPECT_JVM_ARG:--XX:+ExitOnOutOfMemoryError}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"

log "Waiting for ${NEO4J_RESOURCE} Installed condition (config ConfigMap rendered)"
if ! kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} Installed condition not True within ${TIMEOUT}"
fi

# One ConfigMap key per setting (Helm parity); the value is the newline-joined argument list.
rendered="$(kubectl get configmap "${CONFIGMAP}" -n "${NEO4J_NAMESPACE}" \
  -o 'jsonpath={.data.server\.jvm\.additional}' 2>/dev/null || true)"
[[ -n "${rendered}" ]] \
  || die "${SETTING_NAME} missing from ConfigMap ${CONFIGMAP} (useDefaults true must render it)"

for arg in "${EXPECT_DEFAULT_ARGS[@]}"; do
  grep -qxF -- "${arg}" <<<"${rendered}" \
    || die "ConfigMap ${CONFIGMAP} ${SETTING_NAME} lacks Neo4j default '${arg}'; got: ${rendered}"
done
grep -qxF -- "${EXPECT_JVM_ARG}" <<<"${rendered}" \
  || die "ConfigMap ${CONFIGMAP} ${SETTING_NAME} lacks user argument '${EXPECT_JVM_ARG}'; got: ${rendered}"

default_line="$(grep -nxF -- "${EXPECT_DEFAULT_ARGS[0]}" <<<"${rendered}" | head -1 | cut -d: -f1)"
user_line="$(grep -nxF -- "${EXPECT_JVM_ARG}" <<<"${rendered}" | head -1 | cut -d: -f1)"
[[ "${user_line}" -gt "${default_line}" ]] \
  || die "user argument '${EXPECT_JVM_ARG}' (line ${user_line}) must come after the defaults (line ${default_line}); got: ${rendered}"

log "ConfigMap ${CONFIGMAP} carries the Neo4j default JVM arguments ahead of the user argument"

log "Waiting for ${NEO4J_RESOURCE} Ready (Neo4j must accept connections for SHOW SETTINGS)"
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
for arg in "${EXPECT_DEFAULT_ARGS[@]}"; do
  conn_assert_setting localhost "${password}" "${SETTING_NAME}" "${arg}" "config-jvm-defaults"
done
conn_assert_setting localhost "${password}" "${SETTING_NAME}" "${EXPECT_JVM_ARG}" "config-jvm-defaults"

log "jvm.useDefaults true — Neo4j default JVM arguments effective at runtime in ${SETTING_NAME} (NEO-3-003-JVM-01)"
