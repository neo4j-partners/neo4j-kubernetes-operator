#!/usr/bin/env bash
# Helpers for cluster scale tests (NEO-2-011 / AC-NEO-SCALE-*).
#
# Only secondary pools are a supported scale unit — the operator gates primary
# 1<->N growth (domain/formation, reason UnsupportedSystemScaleUp). These helpers
# therefore resize a named secondary pool and verify the outcome three ways:
#   1. the pool StatefulSet reaches the requested replica count,
#   2. Neo4j itself reports that many pool members Enabled+Available
#      (i.e. the operator ran ENABLE SERVER / drain, not just resized the STS), and
#   3. the operator still considers the cluster formed (ClusterFormed=True).
#
# Inputs (from the cluster-scale case fragment):
#   SCALE_POOL       — secondary pool name (e.g. "read")
#   SCALE_SPEC_PATH  — JSON pointer to that pool's members field
#   NEO4J_CR_NAME / NEO4J_NAMESPACE / NEO4J_STS_NAME (primary pool, for cypher)

# scale_pool_sts — StatefulSet backing the pool under test (<cr>-<pool>).
scale_pool_sts() {
  printf '%s' "${NEO4J_CR_NAME}-${SCALE_POOL:?SCALE_POOL not set}"
}

# scale_patch_members <count> — resize the pool via a JSON-patch on the CR.
scale_patch_members() {
  local count=$1
  log "Patching ${SCALE_SPEC_PATH} -> ${count} (pool ${SCALE_POOL})"
  kubectl patch "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --type json \
    -p "[{\"op\":\"replace\",\"path\":\"${SCALE_SPEC_PATH}\",\"value\":${count}}]" >/dev/null \
    || die "failed to patch ${SCALE_SPEC_PATH} to ${count}"
}

# scale_count_pool_servers <password> — how many members of SCALE_POOL Neo4j reports
# as Enabled+Available. Pool members are addressed <cr>-<pool>-N.<ns>.svc...
scale_count_pool_servers() {
  local password=$1 out
  # -d system is mandatory, same as assert/cluster-formed: a direct bolt:// session
  # defaults to the `neo4j` database, which only a subset of members may host. On a pod
  # that does not host it the session is refused and this returns 0 — indistinguishable
  # from "the pool really has no members". 2>&1 keeps the reason for the failure dump.
  out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${NEO4J_STS_NAME}-0" -c neo4j -- bash -c \
    "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
     'SHOW SERVERS YIELD name,address,state,health;'" 2>&1 || true)"
  grep -c "${NEO4J_CR_NAME}-${SCALE_POOL}-.*\"Enabled\".*\"Available\"" <<<"${out}" || true
}

# scale_assert_members <count> <ac-label> — wait for the pool to settle at <count>
# members: StatefulSet replicas, Neo4j-reported Enabled+Available members, and a
# still-formed cluster.
scale_assert_members() {
  local want=$1 ac=$2
  local sts timeout password
  sts="statefulset/$(scale_pool_sts)"
  timeout="${E2E_SCALE_TIMEOUT:-600}"
  password="$(neo4j_password)"

  # 1. StatefulSet reaches the requested size.
  log "Waiting up to ${timeout}s for ${sts} to report ${want} replica(s)"
  local deadline=$((SECONDS + timeout)) replicas=""
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    replicas="$(kubectl get "${sts}" -n "${NEO4J_NAMESPACE}" \
      -o jsonpath='{.spec.replicas}' 2>/dev/null || true)"
    [[ "${replicas}" == "${want}" ]] && break
    sleep 5
  done
  [[ "${replicas}" == "${want}" ]] \
    || die "${sts} replicas=${replicas:-none} after ${timeout}s, expected ${want}"
  log "${sts} declares ${want} replica(s)"

  # 2. Neo4j agrees — the operator actually enabled/drained members, which is the
  #    part a StatefulSet resize alone would not prove.
  log "Waiting for Neo4j to report ${want} '${SCALE_POOL}' member(s) Enabled+Available"
  deadline=$((SECONDS + timeout))
  local count=0
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    count="$(scale_count_pool_servers "${password}")"
    [[ "${count:-0}" -eq "${want}" ]] && break
    sleep 10
  done
  if [[ "${count:-0}" -ne "${want}" ]]; then
    # stdout+stderr: a bare count hides a refused session behind "0 members".
    log "last SHOW SERVERS attempt (stdout+stderr) was:"
    kubectl exec -n "${NEO4J_NAMESPACE}" "${NEO4J_STS_NAME}-0" -c neo4j -- bash -c \
      "cypher-shell -a bolt://localhost:7687 -d system -u neo4j -p '${password}' --format plain \
       'SHOW SERVERS YIELD name,address,state,health;'" >&2 || true
    die "Neo4j reports ${count:-0} '${SCALE_POOL}' member(s) Enabled+Available after ${timeout}s, expected ${want}"
  fi
  log "Neo4j reports ${count} '${SCALE_POOL}' member(s) Enabled+Available"

  # 3. The cluster must still be formed after the topology change.
  local formed reason
  deadline=$((SECONDS + timeout))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    formed="$(kubectl get "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" \
      -o jsonpath='{.status.conditions[?(@.type=="ClusterFormed")].status}' 2>/dev/null || true)"
    [[ "${formed}" == "True" ]] && break
    sleep 5
  done
  reason="$(kubectl get "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.conditions[?(@.type=="ClusterFormed")].reason}' 2>/dev/null || true)"
  [[ "${formed}" == "True" ]] \
    || die "ClusterFormed=${formed:-<absent>} (reason=${reason:-none}) after scaling to ${want}"

  log "Pool '${SCALE_POOL}' settled at ${want} member(s), cluster still formed (${ac})"
}
