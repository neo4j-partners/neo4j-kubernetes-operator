#!/usr/bin/env bash
# case_teardown/trust/cleanup-byo — remove the BYO bolt TLS Secrets provisioned for the case.
# They are not owned by the CR (the user supplies them), so cleanup/standalone's CR delete does
# not GC them. Best-effort: teardown must never fail the suite.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete secret tls-byo-bolt-key tls-byo-bolt-cert -n "${NEO4J_NAMESPACE}" \
  --ignore-not-found >/dev/null 2>&1 || true

log "BYO trust Secret cleanup done"
