#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

fixture="${REPO_ROOT}/${NEO4J_STANDALONE_FIXTURE}"
rendered="$(mktemp)"
stderr_file="$(mktemp)"

# Optional additionalMounts placeholders (only the storage additionalMounts fixture
# carries __MOUNT_NAME__/__MOUNT_PATH__). A fresh random name proves the operator honors
# an arbitrary user-chosen mount; the substitutions are no-ops for other fixtures.
MOUNT_NAME="${STORAGE_MOUNT_NAME:-e2e-extra-${RANDOM}${RANDOM}}"
MOUNT_PATH="${STORAGE_MOUNT_PATH:-/mnt/${MOUNT_NAME}}"

if [[ "${NEO4J_USE_STORAGE_CLASS:-false}" == "true" && -n "${STORAGE_CLASS_NAME:-}" ]]; then
  sed -e "s|name: __CR_NAME__|name: ${NEO4J_CR_NAME}|" \
      -e "s|size: __DATA_SIZE__|size: ${NEO4J_DATA_SIZE:-10Gi}|" \
      -e "s|storageClassName: __STORAGE_CLASS__|storageClassName: ${STORAGE_CLASS_NAME}|g" \
      -e "s|__MOUNT_NAME__|${MOUNT_NAME}|g" \
      -e "s|__MOUNT_PATH__|${MOUNT_PATH}|g" \
    "${fixture}" >"${rendered}"
else
  sed -e "s|name: __CR_NAME__|name: ${NEO4J_CR_NAME}|" \
      -e "s|size: __DATA_SIZE__|size: ${NEO4J_DATA_SIZE:-10Gi}|" \
      -e '/storageClassName: __STORAGE_CLASS__/d' \
      -e "s|__MOUNT_NAME__|${MOUNT_NAME}|g" \
      -e "s|__MOUNT_PATH__|${MOUNT_PATH}|g" \
    "${fixture}" >"${rendered}"
fi

# Persist the resolved mount name/path so assert/storage-additional (a separate
# subprocess) can verify the exact point inside the neo4j container.
_deploy_state_dir="$(_apply_state_dir)"
mkdir -p "${_deploy_state_dir}"
printf '%s' "${MOUNT_NAME}" >"${_deploy_state_dir}/${SUITE_CASE_ID:-case}.mount-name"
printf '%s' "${MOUNT_PATH}" >"${_deploy_state_dir}/${SUITE_CASE_ID:-case}.mount-path"

log "Applying ${NEO4J_KIND} CR ${NEO4J_CR_NAME} in namespace ${NEO4J_NAMESPACE} (expect=${CASE_EXPECT:-success})"

set +e
kubectl apply -n "${NEO4J_NAMESPACE}" -f "${rendered}" 2>"${stderr_file}"
apply_exit=$?
set -e

# Persist to files so a later assert (separate subprocess) can read the outcome.
apply_stderr="$(cat "${stderr_file}")"
record_apply_result "${apply_exit}" "${stderr_file}"

rm -f "${rendered}" "${stderr_file}"

if [[ "${CASE_EXPECT:-success}" == "failure" ]]; then
  log "kubectl apply exited ${apply_exit} (expected failure)"
  exit 0
fi

[[ "${apply_exit}" -eq 0 ]] || die "kubectl apply failed (exit ${apply_exit}): ${apply_stderr}"
log "kubectl apply succeeded"
