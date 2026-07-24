#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

fixture="${REPO_ROOT}/${NEO4J_STANDALONE_FIXTURE}"
rendered="$(mktemp)"
stderr_file="$(mktemp)"

if [[ "${NEO4J_USE_STORAGE_CLASS:-false}" == "true" && -n "${STORAGE_CLASS_NAME:-}" ]]; then
  sed -e "s|name: __CR_NAME__|name: ${NEO4J_CR_NAME}|" \
      -e "s|size: __DATA_SIZE__|size: ${NEO4J_DATA_SIZE:-10Gi}|" \
      -e "s|storageClassName: __STORAGE_CLASS__|storageClassName: ${STORAGE_CLASS_NAME}|g" \
    "${fixture}" >"${rendered}"
else
  sed -e "s|name: __CR_NAME__|name: ${NEO4J_CR_NAME}|" \
      -e "s|size: __DATA_SIZE__|size: ${NEO4J_DATA_SIZE:-10Gi}|" \
      -e '/storageClassName: __STORAGE_CLASS__/d' \
    "${fixture}" >"${rendered}"
fi

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
