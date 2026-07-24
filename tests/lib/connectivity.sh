#!/usr/bin/env bash
# Connectivity probes for Neo4j connectors (bolt, neo4j routing, http, https).
#
# Protocol coverage vs port (BDR-007 / render/connectivity):
#   bolt  -> 7687 (binary) — probed with cypher-shell bolt://
#   neo4j -> 7687 (routing) — probed with cypher-shell neo4j://
#   http  -> 7474 — probed with a raw HTTP/1.0 request over bash /dev/tcp
#   https -> 7473 — probed with a raw TCP connect (present only when TLS is on)
#
# Probes execute *inside a target container* so the same logic works from the
# Neo4j pod itself (localhost) and from a separate client pod (Service DNS).
# The caller sets CONN_EXEC_FN to a function that runs a bash snippet in the
# chosen target and returns its exit code.
#
# The expected outcome per protocol is data-driven via EXPECT_CONN_<PROTO>
# (success|failure). Defaults encode the no-TLS matrix: bolt/neo4j/http succeed,
# https fails because the connector is not exposed without TLS.

CONN_BOLT_PORT="${CONN_BOLT_PORT:-7687}"
CONN_HTTP_PORT="${CONN_HTTP_PORT:-7474}"
CONN_HTTPS_PORT="${CONN_HTTPS_PORT:-7473}"

# neo4j_password reads the bootstrap password from the auth Secret (NEO4J_AUTH=neo4j/<pw>).
neo4j_password() {
  local raw
  raw="$(kubectl get secret "${NEO4J_AUTH_SECRET}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.data.NEO4J_AUTH}' 2>/dev/null | base64 -d 2>/dev/null)"
  [[ -n "${raw}" ]] || die "could not read ${NEO4J_AUTH_SECRET}/NEO4J_AUTH"
  printf '%s' "${raw#neo4j/}"
}

# conn_expect <proto> — resolves the expected outcome for a protocol.
conn_expect() {
  local proto=$1 var
  var="EXPECT_CONN_$(printf '%s' "${proto}" | tr '[:lower:]' '[:upper:]')"
  local val="${!var:-}"
  if [[ -n "${val}" ]]; then
    printf '%s' "${val}"
    return 0
  fi
  case "${proto}" in
    https) printf 'failure' ;;
    *) printf 'success' ;;
  esac
}

# conn_probe <proto> <host> <password> — run the probe in the target; exit 0 = reachable.
conn_probe() {
  local proto=$1 host=$2 pw=$3 snippet
  case "${proto}" in
    bolt)
      snippet="cypher-shell -a 'bolt://${host}:${CONN_BOLT_PORT}' -u neo4j -p '${pw}' --format plain 'RETURN 1;'"
      ;;
    neo4j)
      snippet="cypher-shell -a 'neo4j://${host}:${CONN_BOLT_PORT}' -u neo4j -p '${pw}' --format plain 'RETURN 1;'"
      ;;
    http)
      snippet="exec 3<>/dev/tcp/${host}/${CONN_HTTP_PORT} && printf 'GET / HTTP/1.0\r\nHost: neo4j\r\n\r\n' >&3 && head -c 128 <&3 | grep -qi 'HTTP/'"
      ;;
    https)
      snippet="exec 3<>/dev/tcp/${host}/${CONN_HTTPS_PORT}"
      ;;
    *)
      die "unknown protocol: ${proto}"
      ;;
  esac
  "${CONN_EXEC_FN}" "${snippet}"
}

# conn_assert_one <proto> <expect> <host> <password> <label> — probe and enforce expectation.
# Expected-success probes retry (Neo4j may still be warming up); expected-failure is checked once.
conn_assert_one() {
  local proto=$1 expect=$2 host=$3 pw=$4 label=$5
  local out ok=1 max=1 i
  [[ "${expect}" == "success" ]] && max="${CONN_RETRIES:-20}"

  for ((i = 1; i <= max; i++)); do
    if out="$(conn_probe "${proto}" "${host}" "${pw}" 2>&1)"; then
      ok=0
      break
    fi
    [[ "${expect}" == "success" && "${i}" -lt "${max}" ]] && sleep "${CONN_RETRY_DELAY:-3}"
  done

  if [[ "${expect}" == "success" ]]; then
    [[ "${ok}" -eq 0 ]] \
      || die "[${label}] ${proto}://${host}: expected SUCCESS but failed after ${max} attempts: ${out}"
    log "[${label}] ${proto}: reachable (expected success)"
  else
    [[ "${ok}" -ne 0 ]] \
      || die "[${label}] ${proto}://${host}: expected FAILURE but the connection succeeded"
    log "[${label}] ${proto}: refused (expected failure)"
  fi
}

# conn_assert_matrix <host> <label> — probe bolt, neo4j, http, https against expectations.
conn_assert_matrix() {
  local host=$1 label=$2 pw proto
  pw="$(neo4j_password)"
  log "[${label}] probing ${host} (bolt/neo4j/http/https)"
  for proto in bolt neo4j http https; do
    conn_assert_one "${proto}" "$(conn_expect "${proto}")" "${host}" "${pw}" "${label}"
  done
  log "[${label}] connectivity matrix passed"
}
