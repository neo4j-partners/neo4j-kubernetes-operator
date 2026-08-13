#!/usr/bin/env bash
# assert/plugins-license — BDR-004 invariant 2: a licensed plugin's Secret is materialised
# on the pod that runs the plugin.
#
# Mount only. The licence content is a dummy and is not inspected — this asserts that the
# operator wires the Secret onto the workload, not that the plugin accepts a licence
# (which would need real licence material and cannot run in CI).
#
# The path is still meaningful: render/workload/plugin_volumes.go mounts each licensed
# plugin's Secret at /licenses/<pluginID>, so a regression that changed the directory or
# dropped the volume shows up here.
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
LICENSE_DIR="/licenses/${PLUGIN_ID}"

storage_wait_ready

# The Secret is projected as its own mount, not written into the image filesystem.
storage_assert_mountpoint "${LICENSE_DIR}" plugins-license

log "Licence Secret for '${PLUGIN_ID}' mounted at ${LICENSE_DIR} (BDR-004 invariant 2)"
