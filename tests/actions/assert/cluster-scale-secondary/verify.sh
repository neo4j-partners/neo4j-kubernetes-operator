#!/usr/bin/env bash
# assert/cluster-scale-secondary — NEO-3-011-SRV-01 (AC-NEO-SCALE), the secondary half of the
# resize path: a read pool is grown 1 -> 2 and shrunk back to 1 on a single-primary cluster.
#
# assert/cluster-scale-out-in covers the primary pool, and that is a genuinely different drain. A
# departing primary has copies to hand over and a raft membership to leave, so Neo4j walks it to
# the Deallocated state on its own. A departing secondary ends up hosting nothing but `system`,
# and Neo4j can leave it labelled Deallocating indefinitely — the operator therefore decides from
# what the member still hosts (SHOW SERVERS.hosting), which is Neo4j's own definition of that
# state. Waiting on the label alone held the StatefulSet at its old size forever, which is the
# regression this case exists to catch.
#
# The scale-in outcome is what is enforced: the pool reaches its target within the budget, the
# shrink went through the drain gate (status.drainOK, ADD-02) rather than around it, and
# ServersPendingDrain ends False/NoDrain. A drain that outlived the operator's budget reports
# DrainTimeout instead, and this case fails naming it.
#
# The primary pool must not even notice, in either direction. Resizing a secondary pool moves
# initial.dbms.default_secondaries_count, which every pool carries and Neo4j only reads when the
# DBMS is initialised — so it is kept out of the config checksum on purpose. If it were not, a
# read resize would roll the primary, and on one primary that is a full outage for nothing. Same
# pod UID, same restart count, same checksum, before and after.
#
# No-op unless the case declares SECONDARY_SCALE_POOL: the workload-cluster pipeline runs every
# assert for every case, and the other cases have no secondary pool to resize.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CLIENT_SVC, NEO4J_AUTH_SECRET,
#         SECONDARY_SCALE_POOL, SECONDARY_SCALE_OUT_MEMBERS, SECONDARY_SCALE_IN_MEMBERS,
#         SECONDARY_SCALE_TIMEOUT, CLUSTER_EXPECTED_MEMBERS
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

if [[ -z "${SECONDARY_SCALE_POOL:-}" ]]; then
  log "skip cluster-scale-secondary: case declares no SECONDARY_SCALE_POOL"
  exit 0
fi

POOL="${SECONDARY_SCALE_POOL}"
WIDE="${SECONDARY_SCALE_OUT_MEMBERS:?SECONDARY_SCALE_OUT_MEMBERS not set — needed as the scale-out target}"
NARROW="${SECONDARY_SCALE_IN_MEMBERS:-1}"
PRIMARIES="${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set — needed to count the members Neo4j should admit}"
# Each half creates or drains a member and waits for the cluster to settle, so this is a
# formation-sized budget, not an assertion-sized one.
TIMEOUT_SECS="${SECONDARY_SCALE_TIMEOUT:-900}"

# Catalogued in src/internal/oracle/catalog.go — the reasons are the contract, the messages are not,
# and oracle_require turns a rename in the catalog into a failure here instead of a timeout later.
NO_DRAIN_REASON=NoDrain
DRAIN_TIMEOUT_REASON=DrainTimeout
oracle_require ServersPendingDrain "${NO_DRAIN_REASON}"
oracle_require ServersPendingDrain "${DRAIN_TIMEOUT_REASON}"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
POOL_STS="statefulset/${NEO4J_CR_NAME}-${POOL}"
PRIMARY_STS="statefulset/${NEO4J_STS_NAME}"
# Cypher goes through the client Service: SHOW SERVERS is a system database read, and only the
# system leader answers reliably (Neo.ClientError.Cluster.NotALeader elsewhere).
POD="${NEO4J_STS_NAME}-0"
HOST="${NEO4J_CLIENT_SVC}.${NEO4J_NAMESPACE}.svc"

password="$(neo4j_password)"

cypher() {
  kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
    "cypher-shell -a 'neo4j://${HOST}:7687' -d system -u neo4j -p '${password}' --format plain \"$1\"" 2>&1 || true
}

cond() { # <condition type> <field>
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

pool_replicas() {
  kubectl get "${POOL_STS}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true
}

# Every pod of the pool, Terminating included — a scale-in is only done once they are gone. Selected
# by neo4j.com/pool, the StatefulSet's own selector label: a name pattern would also match the
# primary pods whenever the CR name happens to end with the pool name.
pool_pods() {
  kubectl get pods -n "${NEO4J_NAMESPACE}" --no-headers 2>/dev/null \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME},neo4j.com/pool=${POOL}" \
    | wc -l | tr -d ' '
}

# Neo4j's own count of usable members, primaries and secondaries together. Dropped and
# Deallocating servers may linger in SHOW SERVERS, but never as Enabled + Available.
enabled_available() {
  grep -c '"Enabled".*"Available"' <<<"$(cypher 'SHOW SERVERS YIELD name,address,state,health;')" || true
}

dump_state() {
  log "cluster state at failure:"
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" -o yaml >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  # hosting is the column the operator decides on, so it is the one to read when it did not.
  printf 'SHOW SERVERS:\n%s\n' "$(cypher 'SHOW SERVERS YIELD name,address,state,health,hosting;')" >&2
  printf 'SHOW DATABASES:\n%s\n' \
    "$(cypher 'SHOW DATABASES YIELD name, currentStatus, requestedPrimariesCount, requestedSecondariesCount, currentPrimariesCount, currentSecondariesCount;')" >&2
}

scale_pool() { # <members>
  log "Patching ${NEO4J_RESOURCE}: topology.secondaries.${POOL}.members=$1"
  kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
    -p "{\"spec\":{\"topology\":{\"secondaries\":{\"${POOL}\":{\"members\":$1}}}}}" >/dev/null
}

# "<uid> <restarts>" for the primary, plus the config checksum its template carries. A new UID
# means the pod was recreated, a higher restart count means it was restarted in place, and a moved
# checksum means the rendered neo4j.conf changed — the three ways a read resize could disturb it.
primary_identity() {
  printf '%s %s\n' \
    "$(kubectl get "pod/${NEO4J_STS_NAME}-0" -n "${NEO4J_NAMESPACE}" \
      -o jsonpath='{.metadata.uid}' 2>/dev/null || echo absent)" \
    "$(kubectl get "pod/${NEO4J_STS_NAME}-0" -n "${NEO4J_NAMESPACE}" \
      -o jsonpath='{.status.containerStatuses[?(@.name=="neo4j")].restartCount}' 2>/dev/null || echo '?')"
  printf 'checksum %s\n' "$(kubectl get "${PRIMARY_STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.spec.template.metadata.annotations.neo4j\.com/config-checksum}' 2>/dev/null || echo absent)"
}

assert_primary_untouched() { # <baseline> <label>
  local baseline=$1 label=$2 now
  now="$(primary_identity)"
  if [[ "${now}" != "${baseline}" ]]; then
    printf 'before:\n%s\nafter:\n%s\n' "${baseline}" "${now}" >&2
    dump_state
    die "[${label}] resizing the ${POOL} pool disturbed the primary — same pod UID, restart count and config checksum expected. initial.dbms.default_secondaries_count tracks the secondary pools and must stay out of the checksum"
  fi
  log "[${label}] primary untouched (same pod UID, no restart) and config checksum unchanged"
}

# Converged means all three views agree: the pool StatefulSet declares the count, that many pool
# pods exist, and Neo4j admits the primaries plus that many secondaries.
wait_pool() { # <members> <label>
  local want=$1 label=$2 total deadline replicas pods enabled
  total=$((PRIMARIES + want))
  log "[${label}] waiting up to ${TIMEOUT_SECS}s for ${POOL_STS}, its pods and SHOW SERVERS to agree on ${want} member(s) (${total} in total)"
  deadline=$((SECONDS + TIMEOUT_SECS))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    replicas="$(pool_replicas)"
    pods="$(pool_pods)"
    enabled="$(enabled_available)"
    if [[ "${replicas}" == "${want}" && "${pods}" == "${want}" && "${enabled}" == "${total}" ]]; then
      log "[${label}] ${POOL} StatefulSet=${replicas} pods=${pods} Enabled+Available=${enabled}"
      return 0
    fi
    sleep 10
  done
  log "[${label}] last seen: ${POOL} StatefulSet=${replicas:-?} pods=${pods:-?} Enabled+Available=${enabled:-?}, want ${want} and ${total}"
  dump_state
  # Name the failure the way the defect presents itself, so a rerun of the regression reads plainly:
  # a pool held at its old size with the drain still outstanding is exactly the reported hang, and
  # ${DRAIN_TIMEOUT_REASON} is what the operator reports once its own budget is spent.
  if [[ "${replicas:-}" != "${want}" ]]; then
    die "[${label}] ${POOL_STS} still declares ${replicas:-?} member(s) after ${TIMEOUT_SECS}s (want ${want}) — ServersPendingDrain=$(cond ServersPendingDrain status)/$(cond ServersPendingDrain reason), and a drain past the operator's budget reports ${DRAIN_TIMEOUT_REASON}"
  fi
  die "[${label}] the ${POOL} pool did not settle at ${want} member(s) within ${TIMEOUT_SECS}s"
}

# Conditions are written by whichever pass runs next, not at the instant the StatefulSet reaches
# its size, so this waits instead of sampling once.
wait_condition() { # <type> <expected status> <timeout secs> <label>
  local type=$1 want=$2 budget=$3 label=$4 deadline status reason
  deadline=$((SECONDS + budget))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    status="$(cond "${type}" status)"
    if [[ "${status}" == "${want}" ]]; then
      log "[${label}] ${type}=${status} (reason=$(cond "${type}" reason))"
      return 0
    fi
    sleep 5
  done
  reason="$(cond "${type}" reason)"
  dump_state
  die "[${label}] ${type}=${status:-<absent>} (reason=${reason:-none}) after ${budget}s, expected ${want}"
}

wait_ready() { # <label>
  local label=$1
  if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
    -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" >/dev/null 2>&1; then
    dump_state
    die "[${label}] ${NEO4J_RESOURCE} did not return to Ready"
  fi
  [[ "$(cond ClusterFormed status)" == "True" ]] \
    || { dump_state; die "[${label}] Ready but ClusterFormed=$(cond ClusterFormed status) (reason=$(cond ClusterFormed reason))"; }
  log "[${label}] ${NEO4J_RESOURCE} Ready, ClusterFormed=True"
}

# ---------------------------------------------------------------------------
# 1. Baseline
# ---------------------------------------------------------------------------
log "Baseline: ${POOL} pool at $(pool_replicas) member(s), scaling out to ${WIDE} then back in to ${NARROW}"
wait_pool "${NARROW}" "baseline"
BASELINE_PRIMARY="$(primary_identity)"

# ---------------------------------------------------------------------------
# 2. Scale out — the new secondary is enabled by the operator, not merely scheduled
# ---------------------------------------------------------------------------
scale_pool "${WIDE}"
wait_pool "${WIDE}" "scale-out"
wait_ready "scale-out"
assert_primary_untouched "${BASELINE_PRIMARY}" "scale-out"
printf 'SHOW SERVERS (hosting) after scale-out:\n%s\n' \
  "$(cypher 'SHOW SERVERS YIELD name,address,state,hosting;')"

# ---------------------------------------------------------------------------
# 3. Scale in — the departing secondary must be released in Neo4j, then the pool shrinks
# ---------------------------------------------------------------------------
scale_pool "${NARROW}"

# Observation, not a gate: each pass is a 15s requeue, so which drain reason is visible depends on
# when we look. Failing on a missed sample would make this case flaky — the outcome below is what
# is enforced.
log "Watching for the drain to become visible (ServersPendingDrain)"
deadline=$((SECONDS + 240))
seen_drain=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  if [[ "$(cond ServersPendingDrain status)" == "True" ]]; then
    seen_drain="$(cond ServersPendingDrain reason)"
    log "ServersPendingDrain=True (reason=${seen_drain}) while ${POOL_STS} still declares $(pool_replicas) member(s) — Neo4j is released first"
    break
  fi
  [[ "$(pool_replicas)" == "${NARROW}" ]] && break
  sleep 2
done
[[ -n "${seen_drain}" ]] || log "drain not sampled (it can complete between two polls) — checking the outcome instead"

wait_pool "${NARROW}" "scale-in"

# The gate itself: the operator only lets the StatefulSet shrink after writing its own confirmation
# that Neo4j released the departing member. Missing here would mean the pool shrank around the
# drain instead of behind it.
drain_ok="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath="{.status.drainOK.${POOL}}" 2>/dev/null || true)"
[[ "${drain_ok}" == "${NARROW}" ]] \
  || { dump_state; die "status.drainOK.${POOL}='${drain_ok:-absent}', expected ${NARROW} — the StatefulSet shrank without the drain gate (ADD-02)"; }
log "status.drainOK.${POOL}=${drain_ok} — the shrink went through the drain gate"

# A drain that never finished would still be True here, reporting ${DRAIN_TIMEOUT_REASON} once the
# operator's budget is spent.
wait_condition ServersPendingDrain False 180 "scale-in"
reason="$(cond ServersPendingDrain reason)"
[[ "${reason}" == "${NO_DRAIN_REASON}" ]] \
  || { dump_state; die "[scale-in] ServersPendingDrain=False with reason ${reason}, expected ${NO_DRAIN_REASON}"; }

wait_ready "scale-in"
assert_primary_untouched "${BASELINE_PRIMARY}" "scale-in"

printf 'SHOW SERVERS (hosting) after scale-in:\n%s\n' \
  "$(cypher 'SHOW SERVERS YIELD name,address,state,hosting;')"
log "${POOL} pool scaled ${NARROW} -> ${WIDE} -> ${NARROW}: the drained secondary was released by Neo4j and the primary never moved"
