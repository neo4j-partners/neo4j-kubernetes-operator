#!/usr/bin/env bash
# tls/cleanup-certs — drop the TLS Secrets published by tls/ensure-cluster-certs. They are
# not owned by the CR, so cleanup/standalone leaves them behind. Best-effort and idempotent.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete secret -n "${NEO4J_NAMESPACE}" \
  -l "app.kubernetes.io/managed-by=neo4j-e2e" --ignore-not-found >/dev/null 2>&1 || true

log "Removed e2e-managed TLS Secrets, if any"
