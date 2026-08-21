#!/usr/bin/env bash
# assert/cluster-tls — NEO-2-005 / NEO-3-005-TLS-03 (AC-NEO-TLS + AC-NEO-CLUSTER): BYO
# cluster TLS material is accepted, mounted, and in force on the running cluster.
#
# This is the first coverage of the TLS domain at all. Every other cluster fixture sets
# trust.enabled: false with insecureAdminConnection: true — TLS explicitly off — so nothing
# until now exercised spec.trust.
#
# Four levels, cheapest first so a failure names the most specific cause:
#   status  — TLSReady=True/SecretsPresent: the operator resolved every referenced Secret
#             and key. False/SecretMissing here means the material never reached the pod.
#   mount   — the leaf pair and the trusted CA are present inside the container at the
#             paths render/trust derives (/var/lib/neo4j/certificates/<policy>/...).
#   runtime — SHOW SETTINGS reports the policy enabled and client_auth REQUIRE. The image
#             ships its own neo4j.conf, so a rendered ConfigMap key is not proof.
#   effect  — assert/cluster-formed runs alongside this one in the suite. That is the real
#             payoff: cluster comms are mTLS, so if the certificate or its SANs were wrong
#             the handshake would fail and the cluster would never form.
#
# client_auth is REQUIRE and not configurable: render/trust forces it for the cluster policy
# and the validator rejects None, because cluster communication is mutually authenticated.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/storage.sh
source "${SCRIPT_DIR}/../../../lib/storage.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
CERT_DIR="/var/lib/neo4j/certificates/cluster"

storage_wait_ready

# 1. status — the operator accepted the referenced Secrets.
tls_status="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o 'jsonpath={.status.conditions[?(@.type=="TLSReady")].status}' 2>/dev/null || true)"
tls_reason="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o 'jsonpath={.status.conditions[?(@.type=="TLSReady")].reason}' 2>/dev/null || true)"
[[ "${tls_status}" == "True" && "${tls_reason}" == "SecretsPresent" ]] \
  || die "TLSReady=${tls_status:-<absent>}/${tls_reason:-none}, expected True/SecretsPresent — the operator did not accept the trust material"
log "TLSReady=True (SecretsPresent)"

# 2. mount — leaf pair plus the trusted CA, at the paths render/trust derives.
for f in public.crt private.key trusted/ca.crt; do
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- test -r "${CERT_DIR}/${f}" \
    || die "${CERT_DIR}/${f} not readable in the neo4j container — trust material was not mounted"
done
log "Cluster TLS material mounted at ${CERT_DIR} (public.crt, private.key, trusted/ca.crt)"

# The mounted leaf must be the one we published, not something the image shipped.
subject="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- \
  openssl x509 -in "${CERT_DIR}/public.crt" -noout -subject 2>/dev/null || true)"
[[ "${subject}" == *"${NEO4J_CR_NAME}-cluster"* ]] \
  || die "mounted certificate subject is '${subject:-<unreadable>}', expected CN=${NEO4J_CR_NAME}-cluster — wrong certificate mounted"
log "Mounted certificate is the published one (${subject})"

# 3. runtime — the policy is in force on the server, not merely rendered.
conn_exec_serverpod() { kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c "$1"; }
CONN_EXEC_FN=conn_exec_serverpod
password="$(neo4j_password)"

conn_assert_setting localhost "${password}" dbms.ssl.policy.cluster.enabled "true" cluster-tls
conn_assert_setting localhost "${password}" dbms.ssl.policy.cluster.client_auth "REQUIRE" cluster-tls

log "Cluster TLS policy active on the running cluster: enabled=true, client_auth=REQUIRE (NEO-3-005-TLS-03)"
