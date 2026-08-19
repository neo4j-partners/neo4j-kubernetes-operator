#!/usr/bin/env bash
# assert/tls-ready — NEO-2-005: TLS material issued by cert-manager, mounted, and actually
# used. Pods being Running or the operator being happy is not enough; this proves the
# encrypted path end to end:
#
#   1. cert-manager issued every leaf the operator asked for (Certificate Ready=True).
#   2. The operator's own verdict: TLSReady=True/SecretsPresent — it found usable key
#      material in the Secrets cert-manager wrote (src/internal/status: observeTLSReady).
#   3. The cluster formed over that material (ClusterFormed=True — the operator's admin dial
#      speaks TLS to the members).
#   4. Neo4j itself serves Bolt over TLS: SHOW SERVERS via bolt+ssc returns every member
#      Enabled+Available, AND a plaintext bolt:// session is refused (TLS is enforced, not
#      merely offered).
#
# bolt+ssc (not bolt+s): the CA is a private self-signed root not in the pod trust store, so
# full verification would fail; +ssc encrypts and trusts the presented cert, which is what a
# client without the CA does. -d system: SHOW SERVERS is a system-database command.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET,
#         CLUSTER_EXPECTED_MEMBERS, E2E_CLUSTER_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

WANT="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set}"
NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
TIMEOUT_SECS="${E2E_CLUSTER_TIMEOUT:-600}"

cond() {  # cond <type> <field>
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

# 1. Every Certificate the operator created for this CR must be issued. A pending Certificate
#    is cert-manager's own verdict that no usable Secret exists yet — assert it directly so a
#    failure points at issuance, not at Neo4j.
log "Waiting for cert-manager Certificates (instance=${NEO4J_CR_NAME}) to be Ready"
deadline=$((SECONDS + TIMEOUT_SECS))
certs_ok=0
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  # One line per Certificate: "<name> <Ready-status>". No rows yet = operator has not created
  # them; a row with anything but True = still issuing.
  rows="$(kubectl get certificate -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null || true)"
  # awk over grep -c: a here-string appends a trailing newline, which grep -cv counts as a
  # spurious not-ready row. NF skips blank lines; $NF is the Ready status column.
  total="$(awk 'NF{c++} END{print c+0}' <<<"${rows}")"
  notready="$(awk 'NF && $NF!="True"{c++} END{print c+0}' <<<"${rows}")"
  if [[ "${total}" -ge 1 && "${notready}" -eq 0 ]]; then
    certs_ok=1
    break
  fi
  sleep 5
done
if [[ "${certs_ok}" -ne 1 ]]; then
  kubectl get certificate -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  die "cert-manager Certificates for ${NEO4J_CR_NAME} not all Ready within ${TIMEOUT_SECS}s"
fi
log "cert-manager Certificates Ready:"
printf '%s\n' "${rows}" | sed 's/^/  /' >&2

# 2. The operator's own TLS verdict. SecretsPresent means it read usable material out of the
#    issued Secrets; CertificatePending means it is still requeueing on cert-manager.
log "Waiting up to ${TIMEOUT_SECS}s for TLSReady=True (reason SecretsPresent)"
deadline=$((SECONDS + TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(cond TLSReady status)" == "True" ]] && break
  sleep 5
done
tls_status="$(cond TLSReady status)"
tls_reason="$(cond TLSReady reason)"
[[ "${tls_status}" == "True" ]] \
  || die "TLSReady=${tls_status:-<absent>} (reason=${tls_reason:-none}, message=$(cond TLSReady message)) — operator does not consider TLS ready"
[[ "${tls_reason}" == "SecretsPresent" ]] \
  || die "TLSReady=True but reason=${tls_reason} (expected SecretsPresent)"
log "TLSReady=True (SecretsPresent)"

# 3. The operator considers the cluster formed — its admin dial reached the members over TLS.
log "Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_RESOURCE} ClusterFormed"
if ! kubectl wait --for=condition=ClusterFormed "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} ClusterFormed not True within ${TIMEOUT_SECS}s (reason=$(cond ClusterFormed reason))"
fi
log "ClusterFormed=True (reason=$(cond ClusterFormed reason))"

# 4a. Neo4j serves Bolt over TLS. Every member must be Enabled+Available, queried over the
#     encrypted connector. Retried: members are admitted progressively.
password="$(neo4j_password)"
log "Querying SHOW SERVERS over bolt+ssc from ${POD} — expecting ${WANT} Enabled+Available"
deadline=$((SECONDS + TIMEOUT_SECS))
ok=0
servers_out=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  servers_out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt+ssc://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     'SHOW SERVERS YIELD name,address,state,health;'" 2>&1 || true)"
  count="$(grep -c '"Enabled".*"Available"' <<<"${servers_out}" || true)"
  if [[ "${count:-0}" -ge "${WANT}" ]]; then
    ok=1
    break
  fi
  sleep 10
done
if [[ "${ok}" -ne 1 ]]; then
  log "last SHOW SERVERS (bolt+ssc) attempt (stdout+stderr) was:"
  printf '%s\n' "${servers_out:-<no output — cypher-shell did not run or the TLS handshake failed>}" >&2
  die "only ${count:-0}/${WANT} member(s) Enabled+Available over bolt+ssc within ${TIMEOUT_SECS}s"
fi
log "SHOW SERVERS over TLS: ${count}/${WANT} member(s) Enabled+Available"

# 4b. TLS is enforced, not just offered: a plaintext bolt:// session must be refused. Without
#     this a connector that accepted both cleartext and TLS would still pass 4a.
log "Checking plaintext bolt:// is refused (TLS enforced)"
if kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a bolt://localhost:7687 -u neo4j -p '${password}' --format plain 'RETURN 1;'" >/dev/null 2>&1; then
  die "plaintext bolt://localhost:7687 succeeded — TLS is not being enforced on the Bolt connector"
fi
log "plaintext bolt:// refused — Bolt TLS is enforced"

log "TLS ready and in use: cert-manager issued, operator mounted, cluster formed and serving over TLS (NEO-2-005)"
