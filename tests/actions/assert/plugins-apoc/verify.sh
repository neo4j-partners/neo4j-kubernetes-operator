#!/usr/bin/env bash
# assert/plugins-apoc — NEO-3-003-APOC-01: APOC assigned via spec.plugins is callable on the
# running server, not merely declared. Unit tests stop at the NEO4J_PLUGINS env var.
#
# Do not "simplify" this to RETURN apoc.version(): a missing function makes cypher-shell
# print "Unknown function 'apoc.version'", which contains apoc.version and passes a substring
# check. SHOW PROCEDURES is core Cypher and returns FALSE when APOC is absent.
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

conn_assert_cypher localhost "${password}" \
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc.' RETURN count(*) > 0 AS ok;" \
  "TRUE" "plugins-apoc"

# Diagnostic: pins down a future JAR/server version mismatch.
log "apoc.version() -> $(conn_run_cypher localhost "${password}" "RETURN apoc.version();" 2>&1 | tail -1)"
