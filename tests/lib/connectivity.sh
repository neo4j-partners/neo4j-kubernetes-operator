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

# conn_show_setting <host> <pw> <name> — print the effective value of a Neo4j setting via
# SHOW SETTINGS over bolt, run inside CONN_EXEC_FN. Mirrors a manual cypher-shell check:
# reads the *runtime* value Neo4j resolved, not the rendered ConfigMap fragment.
conn_show_setting() {
  local host=$1 pw=$2 name=$3 snippet
  snippet="cypher-shell -a 'bolt://${host}:${CONN_BOLT_PORT}' -u neo4j -p '${pw}' --format plain \"SHOW SETTINGS YIELD name, value WHERE name = '${name}' RETURN value;\""
  "${CONN_EXEC_FN}" "${snippet}"
}

# conn_assert_setting <host> <pw> <name> <expect-substring> <label> — SHOW SETTINGS the
# setting and require the effective value to contain expect-substring. Containment (not
# equality) because Neo4j normalises some values (memory to bytes, lists as [..]). Retries
# because a freshly-Ready server may still be settling.
conn_assert_setting() {
  local host=$1 pw=$2 name=$3 want=$4 label=$5
  local out ok=1 max="${CONN_RETRIES:-20}" i
  for ((i = 1; i <= max; i++)); do
    if out="$(conn_show_setting "${host}" "${pw}" "${name}" 2>&1)"; then
      if grep -qF -- "${want}" <<<"${out}"; then
        ok=0
        break
      fi
    fi
    [[ "${i}" -lt "${max}" ]] && sleep "${CONN_RETRY_DELAY:-3}"
  done
  [[ "${ok}" -eq 0 ]] \
    || die "[${label}] SHOW SETTINGS '${name}' did not contain '${want}' after ${max} attempts; got: ${out}"
  log "[${label}] ${name} effective value contains '${want}' (SHOW SETTINGS)"
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

# Where conn_show_servers parks the last cypher-shell stderr. A file, not a variable:
# callers invoke the helper as "$(conn_show_servers ...)", which runs it in a subshell,
# so anything it assigned would be discarded on return.
CONN_ERR_FILE="${CONN_ERR_FILE:-${TMPDIR:-/tmp}/e2e-cypher-stderr.log}"

# conn_show_servers <pod> <password> — SHOW SERVERS over localhost bolt inside <pod>.
# Prints the rows on stdout.
#
# `-d system` is required, not cosmetic. SHOW SERVERS is a system-scope administration
# command; without -d, cypher-shell opens the session against the user's default database
# (`neo4j`) and the query dies with "graph reference not found" on any member that does
# not resolve that database locally — even while the cluster is perfectly healthy and
# SHOW DATABASES reports neo4j online. That made the check depend on default-database
# availability, which has nothing to do with the cluster membership it asserts.
#
# Callers poll this while a cluster is still forming, so it stays quiet per attempt and
# records the failure instead: cypher-shell's stderr (plus a non-zero exit) lands in
# CONN_ERR_FILE for conn_dump_last_error to print once the caller gives up. Swallowing
# stderr here would make an empty result set and a failed connection look identical,
# which is exactly what turns a red run into an undiagnosable one.
conn_show_servers() {
  local pod=$1 password=$2 out rc=0
  : >"${CONN_ERR_FILE}"
  out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${pod}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -u neo4j -p '${password}' -d system --format plain \
     'SHOW SERVERS YIELD name,address,state,health;'" 2>"${CONN_ERR_FILE}")" || rc=$?
  [[ "${rc}" -eq 0 ]] || printf '(cypher-shell exited %s)\n' "${rc}" >>"${CONN_ERR_FILE}"
  printf '%s' "${out}"
}

# conn_dump_last_error — explain the last conn_show_servers attempt on the failure path.
conn_dump_last_error() {
  if [[ -s "${CONN_ERR_FILE}" ]]; then
    log "last cypher-shell stderr:"
    cat "${CONN_ERR_FILE}" >&2
  else
    log "cypher-shell wrote no stderr and exited 0 — it returned an empty result set"
  fi
}
