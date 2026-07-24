#!/usr/bin/env bash
# assert/storage-claimname — Existing data volume referencing a pre-created PVC by name.
# The StatefulSet pod mounts that exact PVC (no volumeClaimTemplate) and the CR is Ready.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

EXPECT_CLAIM="${STORAGE_EXPECT_CLAIM:-e2e-storage-claim-data}"

storage_wait_ready

claim="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.template.spec.volumes[?(@.name=="data")].persistentVolumeClaim.claimName}' 2>/dev/null || true)"
[[ "${claim}" == "${EXPECT_CLAIM}" ]] \
  || die "data volume claimName='${claim:-<none>}', expected '${EXPECT_CLAIM}'"

# Existing claimName must not also produce a volumeClaimTemplate.
vct="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.volumeClaimTemplates[*].metadata.name}' 2>/dev/null || true)"
[[ -z "${vct}" ]] || die "unexpected volumeClaimTemplate(s) '${vct}' for Existing claimName"

phase="$(storage_pvc_phase "${EXPECT_CLAIM}")"
[[ "${phase}" == "Bound" ]] || die "PVC ${EXPECT_CLAIM} phase='${phase}', expected Bound"

log "Neo4j pod mounts pre-created PVC ${EXPECT_CLAIM} (Bound), no volumeClaimTemplate"
