#!/usr/bin/env bash
# assert/storage-share — logs and metrics use Share mode (reuse the data PVC under a
# per-pod subPathExpr). Verify both are real mount points at /logs and /metrics inside
# the neo4j container, and that no dedicated logs/metrics PVCs were provisioned.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

storage_wait_ready

# Only the data volumeClaimTemplate should exist — Share must not create logs/metrics VCTs.
vcts="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.volumeClaimTemplates[*].metadata.name}' 2>/dev/null || true)"
[[ "${vcts}" == "data" ]] \
  || die "expected only the 'data' volumeClaimTemplate for Share logs/metrics, got '${vcts}'"

storage_assert_mountpoint /logs share-logs
storage_assert_mountpoint /metrics share-metrics

log "Share logs (/logs) and metrics (/metrics) mounted off the data volume"
