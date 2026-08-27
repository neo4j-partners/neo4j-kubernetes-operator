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

# A licensed Bloom that is not served is no use, and the plugin's HTTP endpoint only exists if
# server.unmanaged_extension_classes is set — the image's own default for it never reaches the
# config the server reads, so the fixture declares it the way a user has to.
#
# Unauthenticated, so 401 is the pass: it means the extension is mounted at /bloom. An
# unmounted path answers 404, which is the regression this catches. Raw HTTP over /dev/tcp,
# same as conn_probe, rather than assuming curl/wget exist in the image.
bloom_status="$(conn_exec_serverpod \
  "exec 3<>/dev/tcp/localhost/${CONN_HTTP_PORT} && printf 'GET /bloom/ HTTP/1.0\r\nHost: neo4j\r\n\r\n' >&3 && head -1 <&3" \
  2>/dev/null | tr -d '\r')"
[[ -n "${bloom_status}" ]] \
  || die "[plugins-license] no HTTP response from /bloom/ on port ${CONN_HTTP_PORT}"
[[ "${bloom_status}" != *" 404 "* ]] \
  || die "[plugins-license] GET /bloom/ -> ${bloom_status} — the Bloom server extension is not mounted"
log "[plugins-license] Bloom server extension is served at /bloom/ (${bloom_status})"
