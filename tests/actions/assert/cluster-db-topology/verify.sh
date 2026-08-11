#!/usr/bin/env bash
# assert/cluster-db-topology — topology.defaultPrimariesCount is honoured end to end:
# it seeds initial.dbms.default_primaries_count at bootstrap and is then enforced by
# ALTER DATABASE SET TOPOLOGY. A formed DBMS says nothing about how the default
# database is spread over it — with defaultPrimariesCount=1 a 3-primary cluster hosts
# user data on a single server, and `neo4j://` routing hides it from the routing assert.
#
# Requires both the requested count (the declared target) and the current count (the
# allocation Neo4j actually realised) to match.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_AUTH_SECRET,
#         CLUSTER_EXPECTED_DB_PRIMARIES, CLUSTER_DEFAULT_DB, E2E_ASSERT_TIMEOUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

WANT="${CLUSTER_EXPECTED_DB_PRIMARIES:?CLUSTER_EXPECTED_DB_PRIMARIES not set — cluster cases must declare it}"
DB="${CLUSTER_DEFAULT_DB:-neo4j}"
POD="${NEO4J_STS_NAME}-0"
TIMEOUT="${E2E_ASSERT_TIMEOUT:-300s}"

password="$(neo4j_password)"
log "Querying SHOW DATABASES for ${DB} from ${POD} — expecting ${WANT} primary/-ies"

# SHOW DATABASES returns one row per hosting server; DISTINCT collapses them since the
# counts are database-wide. -d system for the same reason as assert/cluster-formed: the
# default database may not be hosted on this pod.
query="SHOW DATABASES YIELD name, requestedPrimariesCount, currentPrimariesCount \
WHERE name = '${DB}' RETURN DISTINCT requestedPrimariesCount, currentPrimariesCount;"

deadline=$((SECONDS + ${TIMEOUT%s}))
out=""
requested=""
current=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     \"${query}\"" 2>&1 || true)"
  # The result row is two integers; skip the header and any cypher-shell error text.
  # No match means the database is not reported yet — keep polling.
  row="$(grep -E '^ *"?[0-9]+"? *, *"?[0-9]+"? *$' <<<"${out}" | tail -n 1 || true)"
  requested="$(cut -d',' -f1 <<<"${row}" | tr -dc '0-9')"
  current="$(cut -d',' -f2 <<<"${row}" | tr -dc '0-9')"
  if [[ "${requested}" == "${WANT}" && "${current}" == "${WANT}" ]]; then
    log "database ${DB}: requested=${requested} current=${current} primaries — defaultPrimariesCount honoured"
    exit 0
  fi
  sleep 5
done

log "last SHOW DATABASES attempt (stdout+stderr) was:"
printf '%s\n' "${out:-<no output at all — cypher-shell did not run or was refused>}" >&2
kubectl get neo4j "${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" -o yaml >&2 || true
die "database ${DB}: requested=${requested:-?} current=${current:-?} primaries, expected ${WANT} within ${TIMEOUT}"
