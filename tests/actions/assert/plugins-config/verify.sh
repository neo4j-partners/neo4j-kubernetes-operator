#!/usr/bin/env bash
# assert/plugins-config — BDR-004: pluginDefinitions.<id>.config reaches the running server.
#
# This is the per-plugin config layer, distinct from spec.config.apoc: apoc.* keys render
# into the separate <cr>-apoc-config / apoc.conf (covered by feature-config), whereas
# pluginDefinitions.<id>.config merges straight into neo4j.conf, between the operator
# defaults and spec.config.neo4j (render/serverconfig/cluster_defaults.go).
#
# The fixture sets a core-namespaced key on purpose — SHOW SETTINGS does not expose
# plugin-namespaced settings, which is exactly why apoc.conf exists, so an apoc.* key here
# would be unobservable over bolt and could not prove the merge happened.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

# Must match pluginDefinitions.apoc.config in tests/fixtures/neo4j-plugins-config.yaml.
CONFIG_KEY="db.transaction.timeout"
CONFIG_VALUE="47s"

storage_wait_ready

POD="$(storage_pod)"
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

conn_assert_setting localhost "${password}" "${CONFIG_KEY}" "${CONFIG_VALUE}" "plugins-config"

log "pluginDefinitions config (${CONFIG_KEY}=${CONFIG_VALUE}) merged into neo4j.conf and effective at runtime (BDR-004)"
