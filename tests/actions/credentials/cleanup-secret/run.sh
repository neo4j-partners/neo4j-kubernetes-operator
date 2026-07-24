#!/usr/bin/env bash
# credentials/cleanup-secret — remove the pre-created auth Secret (best effort).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

if [[ "${AUTH_SECRET_CREATE:-false}" == "true" && -n "${AUTH_SECRET_NAME:-}" ]]; then
  kubectl delete secret "${AUTH_SECRET_NAME}" -n "${NEO4J_NAMESPACE}" \
    --ignore-not-found >/dev/null 2>&1 || true
  log "Removed auth Secret ${AUTH_SECRET_NAME}"
fi
