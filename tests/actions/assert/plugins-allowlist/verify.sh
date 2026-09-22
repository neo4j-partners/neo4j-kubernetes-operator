#!/usr/bin/env bash
# assert/plugins-allowlist — BDR-004 + NEO-024: assigned plugins are allowlisted, and
# `unrestricted` stays empty. The second half is the security one — unrestricting takes a
# procedure out of the sandbox, so 93bfc63 stopped the operator doing it automatically.
# A regression here widens the sandbox on every plugin install and no render test would notice.
#
# Over bolt, not the ConfigMap: the image ships its own neo4j.conf, so a rendered key is not
# proof the server used it.
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

conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "apoc.*" plugins-allowlist
conn_assert_setting localhost "${password}" dbms.security.procedures.allowlist "gds.*" plugins-allowlist

# SHOW SETTINGS renders an unset value as "".
unrestricted="$(conn_show_setting localhost "${password}" dbms.security.procedures.unrestricted 2>/dev/null | tail -1 | tr -d '"[:space:]')"
[[ -z "${unrestricted}" ]] \
  || die "dbms.security.procedures.unrestricted is '${unrestricted}' — the operator must not unrestrict plugin procedures (NEO-024)"

log "Plugins allowlisted and left sandboxed (unrestricted empty)"
