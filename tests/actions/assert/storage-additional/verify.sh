#!/usr/bin/env bash
# assert/storage-additional — a caller-defined additionalMounts entry (random volume
# name) is mounted at its mountPath inside the neo4j container. The resolved name/path
# are persisted by deploy/standalone (random per run) and read back here.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"

mount_name="$(read_mount_name)"
mount_path="$(read_mount_path)"
[[ -n "${mount_name}" && -n "${mount_path}" ]] \
  || die "additionalMounts name/path not recorded by deploy (name='${mount_name}', path='${mount_path}')"

log "Verifying additionalMounts volume '${mount_name}' at '${mount_path}'"

storage_wait_ready

# The pod template must declare the extra volume under the random name.
vol="$(kubectl get statefulset "${NEO4J_STS_NAME}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath="{.spec.template.spec.volumes[?(@.name==\"${mount_name}\")].name}" 2>/dev/null || true)"
[[ "${vol}" == "${mount_name}" ]] \
  || die "pod has no volume named '${mount_name}' (got '${vol:-<none>}')"

storage_assert_mountpoint "${mount_path}" additional

log "additionalMounts volume '${mount_name}' mounted at '${mount_path}'"
