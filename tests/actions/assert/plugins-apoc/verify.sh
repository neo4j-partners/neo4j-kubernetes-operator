#!/usr/bin/env bash
# assert/plugins-apoc — NEO-3-003-APOC-01 (BDR-004): APOC assigned via spec.plugins is
# actually usable on the running server, not merely declared.
#
# The operator only sets NEO4J_PLUGINS (proven by render/plugins/neo4j_plugins_test.go and
# workload/statefulset_test.go); the image entrypoint installs the JAR at container start,
# from /var/lib/neo4j/labs where the Enterprise image already ships it. So this is the first
# point where a mis-set plugins directory, an incompatible JAR, or a blocked procedure shows
# up at all — and the plugins directory really was mis-set until ensurePluginsMount landed.
#
# Asserts procedure *registration* rather than the output of apoc.version(): a missing
# function makes cypher-shell print "Unknown function 'apoc.version'", which contains the
# string apoc.version and would pass a naive substring check. SHOW PROCEDURES is core
# Cypher, so it returns FALSE instead of erroring when APOC is absent.
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

conn_assert_cypher localhost "${password}" \
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'apoc.' RETURN count(*) > 0 AS ok;" \
  "TRUE" "plugins-apoc"

# Diagnostic only: the version the image actually installed, handy when a future JAR/server
# mismatch turns this case red.
log "apoc.version() -> $(conn_run_cypher localhost "${password}" "RETURN apoc.version();" 2>&1 | tail -1)"

log "APOC assigned via spec.plugins is installed and its procedures are callable (NEO-3-003-APOC-01)"
