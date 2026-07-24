#!/usr/bin/env bash
# assert/storage-vct — Existing data volume via a raw volumeClaimTemplate. The operator
# copies it onto the StatefulSet (VCT "data"), which provisions data-<cr>-server-0.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

storage_wait_ready

vct="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.volumeClaimTemplates[?(@.metadata.name=="data")].metadata.name}' 2>/dev/null || true)"
[[ "${vct}" == "data" ]] \
  || die "StatefulSet ${NEO4J_STS_NAME} has no 'data' volumeClaimTemplate (got '${vct:-<none>}')"

pvc="$(storage_data_pvc)"
phase="$(storage_pvc_phase "${pvc}")"
[[ "${phase}" == "Bound" ]] || die "PVC ${pvc} phase='${phase}', expected Bound"

log "volumeClaimTemplate 'data' provisioned PVC ${pvc} (Bound)"
