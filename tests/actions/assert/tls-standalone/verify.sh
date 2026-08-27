#!/usr/bin/env bash
# assert/tls-standalone — NEO-2-005 (BYO half): a Standalone server serves Bolt over TLS from
# user-supplied Secrets, and the operator itself trusts and dials it.
#
#   1. The operator mounted the BYO material: TLSReady=True/SecretsPresent.
#   2. The CR reached Ready=True/AllMembersReady. This is the SAN gate — the operator dials
#      bolt+s to <cr>.<ns>.svc (formation.ClientBoltURI) and verifies against the supplied cert,
#      so a cert missing that SAN, or key material that does not match, keeps Ready False.
#   3. Neo4j serves Bolt over TLS: a bolt+ssc session runs a query (the CA is a private
#      self-signed leaf not in the pod trust store, so +ssc trusts it; +s would need the CA).
#   4. TLS is enforced, not merely offered: a plaintext bolt:// session is refused.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET, E2E_TLS_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POD="${NEO4J_STS_NAME}-0"
TIMEOUT_SECS="${E2E_TLS_TIMEOUT:-600}"
# Catalogued in src/internal/oracle/catalog.go. Pinned through a variable so the projection lint
# sees it, and checked now rather than after the wait below: a reason renamed in Go then fails by
# name instead of looking like an operator that never mounted the material.
EXPECT_TLS_REASON="${TLS_READY_REASON:-SecretsPresent}"
oracle_require TLSReady "${EXPECT_TLS_REASON}"

cond() {  # cond <type> <field>
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

# 1. Operator mounted the BYO material.
log "Waiting up to ${TIMEOUT_SECS}s for TLSReady=True (reason ${EXPECT_TLS_REASON})"
deadline=$((SECONDS + TIMEOUT_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  [[ "$(cond TLSReady status)" == "True" ]] && break
  sleep 5
done
tls_status="$(cond TLSReady status)"
tls_reason="$(cond TLSReady reason)"
[[ "${tls_status}" == "True" ]] \
  || die "TLSReady=${tls_status:-<absent>} (reason=${tls_reason:-none}, message=$(cond TLSReady message))"
[[ "${tls_reason}" == "${EXPECT_TLS_REASON}" ]] \
  || die "TLSReady=True but reason=${tls_reason} (expected ${EXPECT_TLS_REASON})"
log "TLSReady=True (${EXPECT_TLS_REASON})"

# 2. SAN gate: Ready only goes True once the operator's own bolt+s dial verified the cert.
log "Waiting up to ${TIMEOUT_SECS}s for ${NEO4J_RESOURCE} Ready (operator dials bolt+s and verifies)"
if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
  -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" 2>/dev/null; then
  kubectl describe "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" >&2 || true
  die "${NEO4J_RESOURCE} not Ready within ${TIMEOUT_SECS}s (reason=$(cond Ready reason)) — likely a SAN/trust mismatch on the operator's bolt+s dial"
fi
log "Ready=True (reason=$(cond Ready reason))"

password="$(neo4j_password)"

# 3. Neo4j serves Bolt over TLS. Retried: a just-Ready server can still be settling.
log "Running a query over bolt+ssc from ${POD}"
ok=0
out=""
for _ in $(seq 1 20); do
  out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt+ssc://localhost:7687 -u neo4j -p '${password}' --format plain 'RETURN 1;'" 2>&1 || true)"
  if grep -q '^1$' <<<"${out}"; then
    ok=1
    break
  fi
  sleep 5
done
if [[ "${ok}" -ne 1 ]]; then
  printf '%s\n' "${out:-<no output — cypher-shell did not run or the TLS handshake failed>}" >&2
  die "bolt+ssc query did not succeed within the retry budget — Bolt is not serving over TLS"
fi
log "bolt+ssc query returned 1 — Bolt is serving over TLS"

# 4. TLS is enforced: plaintext bolt:// must be refused.
log "Checking plaintext bolt:// is refused (TLS enforced)"
if kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a bolt://localhost:7687 -u neo4j -p '${password}' --format plain 'RETURN 1;'" >/dev/null 2>&1; then
  die "plaintext bolt://localhost:7687 succeeded — TLS is not being enforced on the Bolt connector"
fi
log "plaintext bolt:// refused — Bolt TLS is enforced"

log "BYO TLS verified: operator mounted user Secrets, dialed bolt+s, and Neo4j serves Bolt over TLS (NEO-2-005)"
