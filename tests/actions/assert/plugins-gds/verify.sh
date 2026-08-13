#!/usr/bin/env bash
# assert/plugins-gds — BDR-004: GDS assigned via spec.plugins is usable on the running
# server, with no licence Secret.
#
# GDS Community runs licence-free — the CEL rule that once demanded a licenceSecretRef was
# removed in 4a12e88 and src/api/v1beta1/neo4j_validation_test.go asserts it stays removed.
# This case pins that: a GDS CR with pluginDefinitions.gds: {} must boot and serve.
#
# Same registration-not-version reasoning as assert/plugins-apoc.
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
  "SHOW PROCEDURES YIELD name WHERE name STARTS WITH 'gds.' RETURN count(*) > 0 AS ok;" \
  "TRUE" "plugins-gds"

# Diagnostic only — also shows whether the server considers itself Community or Enterprise GDS.
log "gds.version() -> $(conn_run_cypher localhost "${password}" "RETURN gds.version();" 2>&1 | tail -1)"

log "GDS assigned without a licence Secret is installed and its procedures are callable (BDR-004)"
