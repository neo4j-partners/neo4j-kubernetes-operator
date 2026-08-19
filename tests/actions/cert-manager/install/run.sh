#!/usr/bin/env bash
# setup/cert-manager — install cert-manager and wait until its webhook can admit objects.
# Idempotent: a repeat apply on a cluster that already has it (e.g. AKS) is a no-op update.
# Runs before operator/install so the operator watches Certificates at boot instead of polling.
#
# Inputs (optional): CERT_MANAGER_VERSION (default pinned), CERT_MANAGER_TIMEOUT.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

VERSION="${CERT_MANAGER_VERSION:-v1.16.2}"
TIMEOUT="${CERT_MANAGER_TIMEOUT:-300s}"
MANIFEST="https://github.com/cert-manager/cert-manager/releases/download/${VERSION}/cert-manager.yaml"

# Already present and healthy? Skip the apply — avoids re-pulling on every case-less rerun
# and avoids churning a cluster-managed install (AKS add-on).
if kubectl get deploy cert-manager-webhook -n cert-manager >/dev/null 2>&1; then
  log "cert-manager already installed — reusing it"
else
  log "Installing cert-manager ${VERSION}"
  kubectl apply -f "${MANIFEST}"
fi

log "Waiting up to ${TIMEOUT} for cert-manager to become available"
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status "deploy/${deploy}" --timeout="${TIMEOUT}" \
    || die "cert-manager deployment ${deploy} did not become available within ${TIMEOUT}"
done

# The webhook being "available" is not the same as it being able to admit — its Service
# endpoints and the CA bundle injection race the rollout. Prove admission works by creating
# a throwaway self-signed Issuer; retry until the webhook stops rejecting the call.
log "Verifying the cert-manager webhook can admit cert-manager.io objects"
probe_ns="${NEO4J_NAMESPACE:-default}"
deadline=$((SECONDS + 120))
while true; do
  if kubectl apply -n "${probe_ns}" -f - >/dev/null 2>&1 <<'EOF'
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: cert-manager-e2e-probe
spec:
  selfSigned: {}
EOF
  then
    kubectl delete issuer cert-manager-e2e-probe -n "${probe_ns}" --ignore-not-found >/dev/null 2>&1 || true
    break
  fi
  [[ "${SECONDS}" -lt "${deadline}" ]] || die "cert-manager webhook not admitting objects within 120s"
  sleep 5
done

log "cert-manager ${VERSION} ready"
