#!/usr/bin/env bash
# Verify cert-manager is installed and its controllers are ready. The heavy
# lifting (rollout wait + webhook admission probe) happens in run.sh; this is a
# fast idempotent assertion so the setup phase fails loudly if it isn't there.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

kubectl get crd certificates.cert-manager.io >/dev/null 2>&1 \
  || die "cert-manager CRDs not found — install did not complete"

for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  ready=$(kubectl get deployment "${deploy}" -n cert-manager \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)
  [[ "${ready:-0}" -ge 1 ]] || die "cert-manager deployment ${deploy} has no ready replicas"
done

log "cert-manager verified ready"
