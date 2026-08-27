#!/usr/bin/env bash
# assert/ports-bolt-only — NEO-3-007-PCMB-01 (AC-NEO-NETWORKING-PORTS-BOLT): a Bolt-only
# profile publishes only the Bolt connector on the client Service. HTTP and HTTPS are not
# exposed (AC-002). Bolt still works — the workload reaches Ready via its Bolt probes and the
# operator manages it over the same client-Service Bolt path (exposure-only; Neo4j keeps
# listening on HTTP internally, it is simply not published).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
SVC="${NEO4J_CLIENT_SVC:-${NEO4J_CR_NAME}}"
# E2E_ASSERT_TIMEOUT already carries its unit (base.sh exports "300s") — use it verbatim, never
# re-append "s" (that yields an invalid "300ss" duration that kubectl rejects instantly).
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"

# Ready proves Bolt accepts connections (probes are Bolt TCP) and the operator could manage the
# workload over the client-Service Bolt path — the core of AC-NEO-NETWORKING-PORTS-BOLT-001.
log "Waiting up to ${TIMEOUT} for ${NEO4J_RESOURCE} Ready (Bolt-only must still serve Bolt)"
kubectl wait --for=condition=Installed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1 \
  || die "${NEO4J_RESOURCE} was not reconciled"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready with a Bolt-only client Service"
fi

kubectl get "service/${SVC}" -n "${NEO4J_NAMESPACE}" >/dev/null 2>&1 \
  || die "client Service ${SVC} not found"

# Space-separated port names on the client Service; wrap in spaces so whole-word grep is exact.
names="$(kubectl get "service/${SVC}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.ports[*].name}' 2>/dev/null || true)"
padded=" ${names} "

grep -qF -- " tcp-bolt " <<<"${padded}" \
  || die "[bolt-only] client Service ${SVC} does not publish tcp-bolt; ports: ${names:-none} (AC-NEO-NETWORKING-PORTS-BOLT-001)"
for absent in tcp-http tcp-https; do
  if grep -qF -- " ${absent} " <<<"${padded}"; then
    die "[bolt-only] client Service ${SVC} unexpectedly publishes ${absent}; ports: ${names} (AC-NEO-NETWORKING-PORTS-BOLT-002)"
  fi
done

log "Bolt-only client Service ${SVC} publishes exactly Bolt (${names}); HTTP/HTTPS not exposed (NEO-3-007-PCMB-01)"
