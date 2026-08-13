#!/usr/bin/env bash
# assert/plugins-allowlist — BDR-004 + NEO-024: the operator allowlists the assigned
# plugins' procedures, and does NOT unrestrict them.
#
# Two halves, and the second is the security one:
#   dbms.security.procedures.allowlist    — generated from the assigned catalog ids
#   dbms.security.procedures.unrestricted — must stay EMPTY
#
# Unrestricting a procedure removes it from the security sandbox, so 93bfc63 stopped the
# operator setting it: it now requires an explicit opt-in via spec.config.neo4j. A
# regression that re-enabled the old behaviour would silently widen the sandbox on every
# plugin install, which no render test would flag as a security change.
#
# Checked over bolt rather than in the ConfigMap: the Neo4j image ships its own neo4j.conf,
# so a rendered key is not proof the server resolved it.
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

# Both assigned plugins appear in the generated allowlist.
conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "apoc.*" plugins-allowlist
conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "gds.*" plugins-allowlist

# NEO-024: the sandbox is not bypassed. SHOW SETTINGS renders an unset value as "".
unrestricted="$(conn_show_setting localhost "${password}" dbms.security.procedures.unrestricted 2>/dev/null | tail -1 | tr -d '"' | tr -d '[:space:]')"
[[ -z "${unrestricted}" ]] \
  || die "dbms.security.procedures.unrestricted is '${unrestricted}' — the operator must not unrestrict plugin procedures; that needs an explicit spec.config.neo4j opt-in (NEO-024)"

log "Assigned plugins are allowlisted and left sandboxed (unrestricted empty) — BDR-004 / NEO-024"
