#!/usr/bin/env bash
# assert/plugins-license — BDR-004 invariant 2: the operator mounts a licensed plugin's
# Secret onto the pod that runs it, at /licenses/<pluginID> (render/workload/plugin_volumes.go).
#
# Mount only. The licence is a dummy, so this does not check contents, file mode, or that
# Bloom accepts it — all of which need real licence material that cannot live in CI.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

# Must match tests/fixtures/neo4j-plugins-bloom.yaml.
PLUGIN_ID="bloom"

storage_wait_ready
storage_assert_mountpoint "/licenses/${PLUGIN_ID}" plugins-license

log "Licence Secret for '${PLUGIN_ID}' mounted at /licenses/${PLUGIN_ID}"
