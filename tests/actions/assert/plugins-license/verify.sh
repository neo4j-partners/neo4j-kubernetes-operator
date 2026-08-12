#!/usr/bin/env bash
# assert/plugins-license — BDR-004 invariant 2: a licensed plugin's Secret is materialised
# on the pod that runs the plugin.
#
# The licence content is a dummy, so this asserts the operator's plumbing, never that Bloom
# validates a licence — that needs real licence material and stays out of CI.
#
# The path is the substance. render/workload/plugin_volumes.go projects the WHOLE Secret
# (no items) at /licenses/<pluginID>, so the file is named after the Secret key:
#   /licenses/bloom/license.key
# docs/02-technical-design/crd-spec/neo4j/spec.md:506 documents /licenses/gds.key instead.
# The code is the contract here; this assert is the executable correction of that doc bug.
# Every shipped example also omits pluginDefinitions.<id>.config, so a licence mounted this
# way is never pointed at and the plugin silently runs Community — see coverage.md.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

# Must match tests/fixtures/neo4j-plugins-bloom.yaml.
PLUGIN_ID="bloom"
SECRET_KEY="license.key"
SECRET_VALUE="e2e-dummy-not-a-real-license"
LICENSE_DIR="/licenses/${PLUGIN_ID}"
LICENSE_FILE="${LICENSE_DIR}/${SECRET_KEY}"

storage_wait_ready

POD="$(storage_pod)"

# The Secret is projected as its own mount, not written into the image filesystem.
storage_assert_mountpoint "${LICENSE_DIR}" plugins-license

# The file is named after the Secret key — /licenses/<id>/<key>, not /licenses/<id>.key.
kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- test -r "${LICENSE_FILE}" \
  || die "licence file ${LICENSE_FILE} not readable — plugin_volumes.go projects the whole Secret, so the filename is the Secret key"

content="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- cat "${LICENSE_FILE}" 2>/dev/null || true)"
[[ "${content}" == "${SECRET_VALUE}" ]] \
  || die "licence file ${LICENSE_FILE} content is '${content}', expected the Secret value"

# Mounted ReadOnly — the workload must not be able to rewrite its own licence.
if kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- \
  sh -c "echo tampered >'${LICENSE_FILE}'" >/dev/null 2>&1; then
  die "licence file ${LICENSE_FILE} is writable — the mount must be ReadOnly"
fi

log "Licence Secret mounted read-only at ${LICENSE_FILE} (BDR-004 invariant 2)"
