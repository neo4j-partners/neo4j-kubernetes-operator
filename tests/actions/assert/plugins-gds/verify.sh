#!/usr/bin/env bash
# assert/plugins-gds — BDR-004: GDS is callable with no licenceSecretRef. GDS Community runs
# licence-free; the CEL rule that once demanded one was removed in 4a12e88, and
# api/v1beta1/neo4j_validation_test.go asserts it stays removed. This pins the runtime half.
#
# SHOW PROCEDURES rather than gds.version() — see assert/plugins-apoc for why.
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
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'gds.' RETURN count(*) > 0 AS ok;" \
  "TRUE" "plugins-gds"

# Diagnostic: also shows whether the server considers itself Community or Enterprise GDS.
log "gds.version() -> $(conn_run_cypher localhost "${password}" "RETURN gds.version();" 2>&1 | tail -1)"
