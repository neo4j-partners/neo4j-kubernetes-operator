#!/usr/bin/env bash
# assert/metrics-scrape — BDR-010: the Prometheus endpoint answers from another pod through the
# admin Service, the path a Prometheus server takes. Probing from inside the Neo4j pod, or through
# kubectl port-forward, would not do: both reach localhost, so an endpoint bound to localhost
# passes them while every real scrape is refused.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
ADMIN_SVC="${NEO4J_CR_NAME}-admin"
METRICS_PORT=2004
SERVER_POD="${NEO4J_STS_NAME}-0"
CLIENT_POD="metrics-client-${SUITE_CASE_ID:-case}"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"

cleanup_client_pod() {
  kubectl delete pod "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
trap cleanup_client_pod EXIT

log "Waiting up to ${TIMEOUT} for ${NEO4J_RESOURCE} Ready"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT}" >/dev/null 2>&1; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} did not become Ready"
fi

port_names="$(kubectl get "service/${ADMIN_SVC}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.ports[*].name}' 2>/dev/null)" \
  || die "admin Service ${ADMIN_SVC} not found"
grep -qw -- tcp-prometheus <<<"${port_names}" \
  || die "admin Service ${ADMIN_SVC} does not publish tcp-prometheus; ports: ${port_names:-none}"

# The Neo4j image is already on the node and ships bash, whose /dev/tcp is all a plain HTTP GET
# needs.
image="$(kubectl get pod "${SERVER_POD}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.spec.containers[?(@.name=="neo4j")].image}' 2>/dev/null)"
[[ -n "${image}" ]] || die "could not resolve Neo4j image from pod ${SERVER_POD}"

log "Launching client pod ${CLIENT_POD}"
cleanup_client_pod
kubectl run "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" \
  --image="${image}" --image-pull-policy=IfNotPresent --restart=Never \
  --command -- sleep 3600 >/dev/null
if ! kubectl wait --for=condition=Ready "pod/${CLIENT_POD}" \
  -n "${NEO4J_NAMESPACE}" --timeout=120s >/dev/null 2>&1; then
  kubectl describe pod "${CLIENT_POD}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "client pod ${CLIENT_POD} did not become Ready"
fi

target="${ADMIN_SVC}.${NEO4J_NAMESPACE}.svc"
log "Scraping http://${target}:${METRICS_PORT}/metrics from ${CLIENT_POD}"
response="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${CLIENT_POD}" -- bash -c "
  exec 3<>/dev/tcp/${target}/${METRICS_PORT} || exit 1
  printf 'GET /metrics HTTP/1.0\r\nHost: ${target}\r\n\r\n' >&3
  timeout 10 cat <&3" 2>&1)" \
  || die "could not connect to ${target}:${METRICS_PORT} — is the endpoint bound to localhost? ${response}"

status="$(head -1 <<<"${response}" | tr -d '\r')"
[[ "${status}" == *" 200 "* ]] \
  || die "GET /metrics through ${ADMIN_SVC} answered '${status}', want 200"
grep -q '^neo4j_' <<<"${response}" \
  || die "GET /metrics through ${ADMIN_SVC} returned no neo4j_* metric family"

log "Prometheus endpoint scraped through ${ADMIN_SVC}: ${status}, $(grep -c '^neo4j_' <<<"${response}") neo4j_* samples"
