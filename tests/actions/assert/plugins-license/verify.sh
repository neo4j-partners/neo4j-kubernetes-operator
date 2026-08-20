#!/usr/bin/env bash
# assert/plugins-license — BDR-004 invariant 2: the operator mounts a licensed plugin's Secret
# onto the pod that runs it, at /licenses/<pluginID> (render/workload/plugin_volumes.go), and
# pluginDefinitions.<id>.config points the plugin at the file it just created.
#
# The runtime half — does the plugin accept that file — runs only when CI exported real licence
# material (LICENSE_GDS / LICENSE_BLOOM). Locally and on fork PRs the fixture carries a dummy,
# so the mount and the config path are all that is provable.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

storage_wait_ready

storage_assert_mountpoint /licenses/gds plugins-license
storage_assert_mountpoint /licenses/bloom plugins-license

POD="$(storage_pod)"
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

# Unlike apoc.*, these two plugin-namespaced settings do reach SHOW SETTINGS, so the path the
# operator merged can be read back from the running server.
conn_assert_setting localhost "${password}" gds.enterprise.license_file /licenses/gds/gds.license plugins-license
conn_assert_setting localhost "${password}" dbms.bloom.license_file /licenses/bloom/bloom.license plugins-license

# Both checks read a boolean rather than the procedure's own words: bloom's status field reports
# "invalid" for a rejected licence, which contains "valid". Its `success` field is FALSE there.
if [[ -n "${LICENSE_GDS:-}" ]]; then
  conn_assert_cypher localhost "${password}" "RETURN gds.isLicensed() AS ok;" TRUE plugins-license
else
  log "[plugins-license] SKIP GDS licence acceptance — LICENSE_GDS unset, the fixture carries a dummy"
fi

if [[ -n "${LICENSE_BLOOM:-}" ]]; then
  conn_assert_cypher localhost "${password}" \
    "CALL bloom.checkLicenseCompliance() YIELD success RETURN success AS ok;" \
    TRUE plugins-license
else
  log "[plugins-license] SKIP Bloom licence acceptance — LICENSE_BLOOM unset, the fixture carries a dummy"
fi
