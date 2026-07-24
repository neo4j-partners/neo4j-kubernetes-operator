#!/usr/bin/env bash
# assert/storage-emptydir — Existing data volume via an inline emptyDir (lab only).
# No PVC and no volumeClaimTemplate are created; /data is backed by ephemeral storage
# and mounted inside the neo4j container.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

storage_wait_ready

# No volumeClaimTemplate and no PVC-backed data volume for an inline emptyDir.
vct="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.volumeClaimTemplates[*].metadata.name}' 2>/dev/null || true)"
[[ -z "${vct}" ]] || die "unexpected volumeClaimTemplate(s) '${vct}' for emptyDir data volume"

claim="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.template.spec.volumes[?(@.name=="data")].persistentVolumeClaim.claimName}' 2>/dev/null || true)"
[[ -z "${claim}" ]] || die "data volume unexpectedly references PVC '${claim}' (expected emptyDir)"

# Confirm the pod actually declares an emptyDir-backed 'data' volume.
if ! kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" -o json 2>/dev/null \
  | grep -q '"emptyDir"'; then
  die "StatefulSet ${NEO4J_STS_NAME} has no emptyDir volume"
fi

storage_assert_mountpoint /data emptydir

log "Inline emptyDir data volume mounted at /data (no PVC, no volumeClaimTemplate)"
