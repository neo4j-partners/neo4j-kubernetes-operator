#!/usr/bin/env bash
# case_teardown/trust/cleanup-byo-cluster — remove the BYO cluster TLS Secrets. Not owned by the
# CR (the user supplies them), so cleanup/standalone's CR delete does not GC them. Best-effort.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete secret tls-byo-cl-cluster tls-byo-cl-bolt -n "${NEO4J_NAMESPACE}" \
  --ignore-not-found >/dev/null 2>&1 || true

log "BYO cluster trust Secret cleanup done"
