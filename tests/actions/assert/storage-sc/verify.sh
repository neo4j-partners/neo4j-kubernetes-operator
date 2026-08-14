#!/usr/bin/env bash
# assert/storage-sc — Dynamic data volume with an explicit storageClassName.
# The operator provisions data-<cr>-server-0 with that StorageClass and the CR reaches Ready.
# The class comes from the cloud profile (local-kind: standard, azure-aks: managed-csi), the
# same source the fixture's __CLOUD_STORAGE_CLASS__ placeholder resolves from.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

EXPECT_SC="${STORAGE_EXPECT_SC:-${STORAGE_CLASS_NAME:-}}"
[[ -n "${EXPECT_SC}" ]] \
  || die "cloud profile ${CLOUD_ID:-unset} sets no STORAGE_CLASS_NAME (set STORAGE_EXPECT_SC to override)"

storage_wait_ready

pvc="$(storage_data_pvc)"
got="$(kubectl get pvc "${pvc}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.storageClassName}' 2>/dev/null || true)"
[[ -n "${got}" ]] || die "data PVC ${pvc} not found"
[[ "${got}" == "${EXPECT_SC}" ]] \
  || die "PVC ${pvc} storageClassName='${got}', expected '${EXPECT_SC}'"

phase="$(storage_pvc_phase "${pvc}")"
[[ "${phase}" == "Bound" ]] || die "PVC ${pvc} phase='${phase}', expected Bound"

log "Dynamic data PVC ${pvc} Bound with storageClassName='${got}'"
