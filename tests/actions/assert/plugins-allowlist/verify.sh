#!/usr/bin/env bash
# assert/plugins-allowlist — BDR-004: the operator derives procedure allowlists from the
# assigned plugin ids, and they are effective on the running server.
#
# Checked over bolt, not in the ConfigMap, and that distinction is the point: the Neo4j image
# ships its own neo4j.conf, so a rendered ConfigMap key is not proof the server resolved it.
# render/serverconfig/cluster_defaults_test.go already covers the render side.
#
# The operator sets both dbms.security.procedures.unrestricted and .allowlist to the same
# value (operator_defaults.go), so both are asserted — a regression that populated only one
# would leave the procedures unusable in one direction.
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

storage_wait_ready

POD="$(storage_pod)"
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

for key in dbms.security.procedures.unrestricted dbms.security.procedures.allowlist; do
  conn_assert_setting localhost "${password}" "${key}" "apoc.*" "plugins-allowlist"
  conn_assert_setting localhost "${password}" "${key}" "gds.*" "plugins-allowlist"
done

log "Procedure allowlists for the assigned plugins are effective on the running server (BDR-004)"
