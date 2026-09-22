#!/usr/bin/env bash
# storage/cleanup-extra — remove prerequisites that are NOT owned by the CR and thus survive
# cleanup/standalone: the pre-created PVC referenced by existing.claimName, and Secrets a
# fixture bundled alongside its CR (feature-plugins ships licence Secrets that way).
# Best-effort and idempotent (no-op when the case created neither).
#
# Scoped to managed-by=neo4j-e2e, which only fixtures set — the operator labels its own
# objects managed-by=neo4j-operator, so nothing the operator owns is in range.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl delete pvc -n "${NEO4J_NAMESPACE}" \
  -l "app.kubernetes.io/managed-by=neo4j-e2e" --ignore-not-found --wait=false >/dev/null 2>&1 || true

kubectl delete secret -n "${NEO4J_NAMESPACE}" \
  -l "app.kubernetes.io/managed-by=neo4j-e2e" --ignore-not-found >/dev/null 2>&1 || true

log "Removed fixture-created PVCs and Secrets (managed-by=neo4j-e2e), if any"
