#!/usr/bin/env bash
# assert/cluster-scale-out-in — NEO-2-011 / NEO-3-011-CSZ-01 / NEO-3-011-SRV-01 (AC-NEO-SCALE):
# one cluster is grown from 3 to 5 primaries and shrunk back to 3, with a database that asks for
# more primaries than the scale-in target sitting on it.
#
# Before anything moves, two resizes must be refused by the API server itself: a pool cannot be
# asked to hold fewer primaries than every standard database is spread over
# (defaultPrimariesCount <= primaries.members), which is also why a 3-primary cluster cannot be
# taken down to one member. A refusal at admission leaves the running cluster untouched.
#
# Scale out proves the new ordinals are more than Running pods: the operator runs ENABLE SERVER,
# so Neo4j itself reports each new member — by name — Enabled + Available, and the CR is Ready.
#
# Scale in proves five things at once — a database wider than the target does not block the
# shrink (the operator narrows database topologies to fit before DEALLOCATE), the StatefulSet is
# *held* at the old size until Neo4j has released the tail servers, the shrink went through that
# gate rather than around it (status.drainOK, ADD-02), the tail was drained one member at a time,
# highest ordinal first, instead of in one bulk DEALLOCATE, and the narrowing was *reported*.
#
# That last one is the point of the DatabaseTopologyResized reason: the operator rewrites a
# topology the user may have set themselves, so the rewrite has to be visible on both surfaces the
# oracle promises — a Warning Event on the CR and an operator log entry naming the database and the
# counts on either side. A resize that works but says nothing fails this case.
#
# The members that stay must not even notice. Both halves check that ordinals 0..NARROW-1 keep their
# pod UID and restart count and that the pool's config checksum never moves: the system bootstrap
# gate never follows primaries.members — derived when topology.minimumMembers is unset, immutable
# when set — precisely so a resize leaves neo4j.conf byte-identical instead of rolling the survivors
# mid-resize.
#
# Note what is *not* one at a time: pods. The pool StatefulSet uses podManagementPolicy Parallel
# on purpose (render/workload/statefulset.go), so the two new pods appear together and the two
# drained ones are removed together. Sequencing lives in the Neo4j operations, not in the kubelet.
#
# The mirror property is checked too, and it is the one users care about most: while the pool is
# wide, the database keeps the topology it was created with. topology.defaultPrimariesCount is 3 in
# this fixture and the database asks for 5, so an operator that treated the field as a constraint
# rather than a creation default would narrow it on the next pass — this case waits a few reconcile
# cycles and fails if it did (TOPO-006).
#
# 3 -> 5 -> 3 on purpose: Neo4j cannot narrow a multi-primary database to a single primary, so
# shrinking to 1 primary is refused (ClusterFormed/UnsupportedSinglePrimary) and is a different
# scenario. See docs/user-guide/03-neo4j/02-clustering.md.
#
# No-op unless the case declares CLUSTER_SCALE_OUT_MEMBERS: the workload-cluster pipeline runs
# every assert for every case, and the other cases must not be resized.
#
# Inputs: NEO4J_CR_NAME, NEO4J_NAMESPACE, NEO4J_STS_NAME, NEO4J_CLIENT_SVC, NEO4J_AUTH_SECRET,
#         CLUSTER_SCALE_OUT_MEMBERS, CLUSTER_SCALE_IN_MEMBERS, CLUSTER_EXPECTED_MEMBERS,
#         CLUSTER_SCALE_DB, CLUSTER_SCALE_TIMEOUT, CLUSTER_SCALE_RESIZE_REASON,
#         CLUSTER_SCALE_STABLE_SECONDS, OPERATOR_NAMESPACE, OPERATOR_LABEL_SELECTOR
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"
# shellcheck source=../../../lib/connectivity.sh
source "${SCRIPT_DIR}/../../../lib/connectivity.sh"

if [[ -z "${CLUSTER_SCALE_OUT_MEMBERS:-}" ]]; then
  log "skip cluster-scale-out-in: case declares no CLUSTER_SCALE_OUT_MEMBERS"
  exit 0
fi

WIDE="${CLUSTER_SCALE_OUT_MEMBERS}"
NARROW="${CLUSTER_SCALE_IN_MEMBERS:-${CLUSTER_EXPECTED_MEMBERS:?CLUSTER_EXPECTED_MEMBERS not set — needed as the scale-in target}}"
DB="${CLUSTER_SCALE_DB:-scalewide}"
# Catalogued in src/internal/oracle/catalog.go; the reason is the stable contract, not the message.
# Event-only, hence the `event` key in the oracle lookup.
RESIZE_REASON="${CLUSTER_SCALE_RESIZE_REASON:-DatabaseTopologyResized}"
oracle_require event "${RESIZE_REASON}"
# Each half creates or drains members and waits for the cluster to settle again, so this is a
# formation-sized budget, not an assertion-sized one.
TIMEOUT_SECS="${CLUSTER_SCALE_TIMEOUT:-900}"

NEO4J_RESOURCE="neo4j/${NEO4J_CR_NAME}"
STS="statefulset/${NEO4J_STS_NAME}"
POD="${NEO4J_STS_NAME}-0"
# Routing address, not bolt://localhost: CREATE DATABASE writes to the system database, which
# only the system leader accepts (Neo.ClientError.Cluster.NotALeader anywhere else). This is the
# same reason the operator dials neo4j:// for its own admin work (formation.AdminBoltURI).
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

sts_replicas() {
  kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.spec.replicas}' 2>/dev/null || true
}

# Every pod of the instance, Terminating included — a scale-in is only done once they are gone.
pod_total() {
  kubectl get pods -n "${NEO4J_NAMESPACE}" \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" --no-headers 2>/dev/null | wc -l | tr -d ' '
}

# Neo4j's own count of usable members. Dropped and Deallocated servers may linger in
# SHOW SERVERS, but never as Enabled + Available, so this is an exact convergence signal.
enabled_available() {
  grep -c '"Enabled".*"Available"' <<<"$(cypher 'SHOW SERVERS YIELD name,address,state,health;')" || true
}

# "<requested> <current>" primaries for DB, or empty while it is not reported yet.
db_primaries() {
  local out row
  out="$(cypher "SHOW DATABASES YIELD name, requestedPrimariesCount, currentPrimariesCount \
WHERE name = '${DB}' RETURN DISTINCT requestedPrimariesCount, currentPrimariesCount;")"
  row="$(grep -E '^ *"?[0-9]+"? *, *"?[0-9]+"? *$' <<<"${out}" | tail -n 1 || true)"
  [[ -n "${row}" ]] || return 0
  printf '%s %s' \
    "$(cut -d',' -f1 <<<"${row}" | tr -dc '0-9')" "$(cut -d',' -f2 <<<"${row}" | tr -dc '0-9')"
}

dump_state() {
  log "cluster state at failure:"
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" -o yaml >&2 || true
  kubectl get pods -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
  printf 'SHOW SERVERS:\n%s\n' "$(cypher 'SHOW SERVERS YIELD name,address,state,health;')" >&2
  printf 'SHOW DATABASES:\n%s\n' \
    "$(cypher 'SHOW DATABASES YIELD name, requestedPrimariesCount, currentPrimariesCount;')" >&2
}

scale_members() { # <members>
  log "Patching ${NEO4J_RESOURCE}: topology.primaries.members=$1"
  kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge \
    -p "{\"spec\":{\"topology\":{\"primaries\":{\"members\":$1}}}}" >/dev/null
}

spec_members() {
  kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.spec.topology.primaries.members}' 2>/dev/null || true
}

# An illegal resize has to be refused by the API server, not absorbed and then reported by the
# operator: a rejected update leaves the running cluster untouched, with nothing to undo.
expect_patch_rejected() { # <patch json> <regex the message must carry> <label>
  local patch=$1 pattern=$2 label=$3 out before
  before="$(spec_members)"
  if out="$(kubectl patch "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" --type merge -p "${patch}" 2>&1)"; then
    dump_state
    die "[${label}] the API server accepted ${patch} — expected a rejection"
  fi
  grep -qE -- "${pattern}" <<<"${out}" \
    || die "[${label}] refused, but the message does not carry /${pattern}/: ${out}"
  [[ "$(spec_members)" == "${before}" ]] \
    || die "[${label}] refused yet spec.topology.primaries.members moved from ${before} to $(spec_members)"
  log "[${label}] refused at admission, members still ${before}: $(tr '\n' ' ' <<<"${out}")"
}

# Neo4j's own verdict per member, matched by pod name inside the advertised address, so this
# names the member that is missing instead of reporting a count that is one short.
assert_members_enabled() { # <first ordinal> <last ordinal> <label>
  local first=$1 last=$2 label=$3 table o
  table="$(cypher 'SHOW SERVERS YIELD name,address,state,health;')"
  for ((o = first; o <= last; o++)); do
    grep -F "${NEO4J_STS_NAME}-${o}." <<<"${table}" | grep -q '"Enabled".*"Available"' \
      || { printf '%s\n' "${table}" >&2; dump_state; die "[${label}] ${NEO4J_STS_NAME}-${o} is not Enabled + Available in SHOW SERVERS"; }
  done
  log "[${label}] ordinals ${first}..${last} each Enabled + Available in Neo4j"
}

assert_members_released() { # <first ordinal> <last ordinal> <label>
  local first=$1 last=$2 label=$3 table o row
  table="$(cypher 'SHOW SERVERS YIELD name,address,state,health;')"
  for ((o = first; o <= last; o++)); do
    row="$(grep -F "${NEO4J_STS_NAME}-${o}." <<<"${table}" || true)"
    if [[ -n "${row}" ]] && grep -q '"Enabled"' <<<"${row}"; then
      printf '%s\n' "${table}" >&2
      dump_state
      die "[${label}] ${NEO4J_STS_NAME}-${o} is still Enabled in Neo4j after the scale-in"
    fi
    log "[${label}] ${NEO4J_STS_NAME}-${o} released (${row:-absent from SHOW SERVERS})"
  done
}

# Narrowing a database is a rewrite of what the user asked for, so it has to be reported on both
# surfaces the oracle promises: a Warning Event on the CR and an operator log entry, each naming the
# database and the counts on either side. A silent ALTER DATABASE is the failure this catches.
assert_resize_reported() { # <from primaries> <to primaries>
  local from=$1 to=$2 events="" event deadline logs entry
  deadline=$((SECONDS + ${E2E_EVENT_WAIT_SECONDS:-60}))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    events="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
      --field-selector "involvedObject.name=${NEO4J_CR_NAME},reason=${RESIZE_REASON}" \
      -o 'jsonpath={range .items[*]}{.type}{"\t"}{.message}{"\n"}{end}' 2>/dev/null || true)"
    [[ -n "${events}" ]] && break
    sleep 3
  done
  [[ -n "${events}" ]] \
    || die "no Event with reason ${RESIZE_REASON} on neo4j/${NEO4J_CR_NAME} — database ${DB} was narrowed silently"

  event="$(grep -F "${DB}" <<<"${events}" | head -1 || true)"
  [[ -n "${event}" ]] \
    || die "${RESIZE_REASON} Events do not name database ${DB}; got: ${events}"
  [[ "${event}" == Warning* ]] \
    || die "the ${RESIZE_REASON} Event for ${DB} should be a Warning; got: ${event}"
  grep -qE "from ${from} primaries" <<<"${event}" \
    || die "Event does not say the topology came from ${from} primaries: ${event}"
  grep -qE "to ${to} " <<<"${event}" \
    || die "Event does not say the topology went to ${to} primaries: ${event}"
  grep -qF 'the scale-in leaves fewer servers' <<<"${event}" \
    || die "Event does not say why the topology was rewritten: ${event}"
  log "Event: ${event}"

  logs="$(kubectl logs -n "${OPERATOR_NAMESPACE:-neo4j-operator-system}" \
    -l "${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}" --tail=-1 2>/dev/null || true)"
  entry="$(grep -F 'database topology resized' <<<"${logs}" | grep -F "${DB}" | head -1 || true)"
  [[ -n "${entry}" ]] \
    || die "operator log has no 'database topology resized' entry for ${DB} — the Event is not backed by a log line"
  for key in fromPrimaries toPrimaries cause; do
    grep -qF -- "${key}" <<<"${entry}" \
      || die "operator log entry for ${DB} lacks the ${key} field: ${entry}"
  done
  log "operator log: ${entry}"
}

# The tail is drained strictly one member at a time, highest ordinal first: the operator returns
# after each Cypher step, so a member is dropped before the next one is touched. Read from the
# operator's own log, which records the order regardless of when we sampled the cluster.
assert_sequential_drain() { # <first drained ordinal> <second drained ordinal>
  local first=$1 second=$2 logs dropped_line next_line first_pod second_pod
  first_pod="${NEO4J_STS_NAME}-${first}"
  second_pod="${NEO4J_STS_NAME}-${second}"
  logs="$(kubectl logs -n "${OPERATOR_NAMESPACE:-neo4j-operator-system}" \
    -l "${OPERATOR_LABEL_SELECTOR:-app.kubernetes.io/name=neo4j-operator}" --tail=-1 2>/dev/null || true)"
  dropped_line="$(grep -nF 'member drained/dropped' <<<"${logs}" | grep -F "${first_pod}" | head -1 | cut -d: -f1)"
  next_line="$(grep -nF 'draining member' <<<"${logs}" | grep -F "${second_pod}" | head -1 | cut -d: -f1)"
  [[ -n "${dropped_line}" ]] \
    || die "operator log has no 'member drained/dropped' entry for ${first_pod} — cannot tell in which order the tail was drained"
  [[ -n "${next_line}" ]] \
    || die "operator log has no 'draining member' entry for ${second_pod}"
  [[ "${dropped_line}" -lt "${next_line}" ]] \
    || die "${second_pod} was drained before ${first_pod} was dropped (log lines ${next_line} and ${dropped_line}) — the tail was not drained one member at a time"
  log "drain order: ${first_pod} dropped (log line ${dropped_line}) before ${second_pod} was touched (line ${next_line})"
}

# "<ordinal> <uid> <restarts>" per surviving member, plus the config checksum the pool template
# carries. Resizing must not touch any of them: a new UID means the pod was recreated, a higher
# restart count means it was restarted in place, and a different checksum means the rendered
# neo4j.conf moved — which is what would roll the pool mid-resize.
pool_identity() { # <last surviving ordinal>
  local last=$1 o
  for ((o = 0; o <= last; o++)); do
    printf '%s %s %s\n' "${o}" \
      "$(kubectl get "pod/${NEO4J_STS_NAME}-${o}" -n "${NEO4J_NAMESPACE}" \
        -o jsonpath='{.metadata.uid}' 2>/dev/null || echo absent)" \
      "$(kubectl get "pod/${NEO4J_STS_NAME}-${o}" -n "${NEO4J_NAMESPACE}" \
        -o jsonpath='{.status.containerStatuses[?(@.name=="neo4j")].restartCount}' 2>/dev/null || echo '?')"
  done
  printf 'checksum %s\n' "$(kubectl get "${STS}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.spec.template.metadata.annotations.neo4j\.com/config-checksum}' 2>/dev/null || echo absent)"
}

# The system bootstrap gate never tracks primaries.members — 3 for every multi-primary cluster unless
# topology.minimumMembers pins it, and that field is immutable — precisely so a resize leaves
# neo4j.conf alone. If it followed the pool, the checksum would move and the surviving members would
# roll while the cluster was being resized.
assert_pool_untouched() { # <baseline snapshot> <last surviving ordinal> <label>
  local baseline=$1 last=$2 label=$3 now
  now="$(pool_identity "${last}")"
  if [[ "${now}" != "${baseline}" ]]; then
    printf 'before:\n%s\nafter:\n%s\n' "${baseline}" "${now}" >&2
    dump_state
    die "[${label}] surviving members were disturbed by the resize — same pod UIDs, restart counts and config checksum expected"
  fi
  log "[${label}] ordinals 0..${last} untouched (same pod UIDs, no restarts) and config checksum unchanged"
}

# Converged means all three views agree: the StatefulSet declares the count, that many pods
# exist, and Neo4j admits exactly that many members.
wait_members() { # <members> <label>
  local want=$1 label=$2 deadline replicas pods enabled
  log "[${label}] waiting up to ${TIMEOUT_SECS}s for StatefulSet, pods and SHOW SERVERS to agree on ${want}"
  deadline=$((SECONDS + TIMEOUT_SECS))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    replicas="$(sts_replicas)"
    pods="$(pod_total)"
    enabled="$(enabled_available)"
    if [[ "${replicas}" == "${want}" && "${pods}" == "${want}" && "${enabled}" == "${want}" ]]; then
      log "[${label}] StatefulSet=${replicas} pods=${pods} Enabled+Available=${enabled}"
      return 0
    fi
    sleep 10
  done
  log "[${label}] last seen: StatefulSet=${replicas:-?} pods=${pods:-?} Enabled+Available=${enabled:-?}, want ${want}"
  dump_state
  die "[${label}] cluster did not settle at ${want} member(s) within ${TIMEOUT_SECS}s"
}

# Conditions are written by whichever reconcile pass runs next, not at the instant the
# StatefulSet reaches its size, so this waits instead of sampling once.
wait_condition() { # <type> <expected status> <timeout secs> <label>
  local type=$1 want=$2 budget=$3 label=$4 deadline status reason
  deadline=$((SECONDS + budget))
  while [[ "${SECONDS}" -lt "${deadline}" ]]; do
    status="$(cond "${type}" status)"
    if [[ "${status}" == "${want}" ]]; then
      reason="$(cond "${type}" reason)"
      log "[${label}] ${type}=${status} (reason=${reason:-?})"
      return 0
    fi
    sleep 5
  done
  reason="$(cond "${type}" reason)"
  dump_state
  die "[${label}] ${type}=${status:-<absent>} (reason=${reason:-none}) after ${budget}s, expected ${want}"
}

wait_settled() { # <label>
  local label=$1 status reason
  if ! kubectl wait --for=condition=Ready "${NEO4J_RESOURCE}" \
    -n "${NEO4J_NAMESPACE}" --timeout="${TIMEOUT_SECS}s" >/dev/null 2>&1; then
    dump_state
    die "[${label}] ${NEO4J_RESOURCE} did not return to Ready"
  fi
  status="$(cond ClusterFormed status)"
  reason="$(cond ClusterFormed reason)"
  [[ "${status}" == "True" ]] \
    || die "[${label}] ClusterFormed=${status:-<absent>} (reason=${reason:-none}) — operator does not consider the resized cluster formed"
  log "[${label}] Ready=True, ClusterFormed=True (reason=${reason:-?})"
}

# ---------------------------------------------------------------------------
# Baseline: the pipeline's earlier asserts already proved this cluster formed.
# ---------------------------------------------------------------------------
log "Baseline: ${NEO4J_RESOURCE} at $(sts_replicas) member(s), scaling out to ${WIDE} then back in to ${NARROW}"
BASELINE_POOL="$(pool_identity "$((NARROW - 1))")"

# ---------------------------------------------------------------------------
# 1. Resizes that must not be accepted at all.
# ---------------------------------------------------------------------------
# A pool cannot hold fewer primaries than every standard database is asked to spread over, so
# defaultPrimariesCount must never exceed primaries.members — on update as well as on create.
expect_patch_rejected \
  "{\"spec\":{\"topology\":{\"defaultPrimariesCount\":${WIDE}}}}" \
  'defaultPrimariesCount' "reject-default-primaries-above-members"

# The same invariant read from the other side, which is also why a 3-primary cluster cannot be
# taken down to a single member: the request never reaches the operator.
expect_patch_rejected \
  '{"spec":{"topology":{"primaries":{"members":1}}}}' \
  'defaultPrimariesCount' "reject-shrink-below-default-primaries"

# ---------------------------------------------------------------------------
# 2. Scale out — grow the primary pool and have Neo4j admit the new members.
# ---------------------------------------------------------------------------
scale_members "${WIDE}"
wait_members "${WIDE}" "scale-out"
wait_settled "scale-out"
assert_members_enabled 0 "$((WIDE - 1))" "scale-out"
assert_pool_untouched "${BASELINE_POOL}" "$((NARROW - 1))" "scale-out"

# ---------------------------------------------------------------------------
# 3. A database as wide as the cluster — the thing a scale-in has to deal with.
# ---------------------------------------------------------------------------
log "Creating database ${DB} with an explicit topology of ${WIDE} primaries"
create_out="$(kubectl exec -n "${NEO4J_NAMESPACE}" "${POD}" -c neo4j -- bash -c \
  "cypher-shell -a 'neo4j://${HOST}:7687' -d system -u neo4j -p '${password}' --format plain \
   \"CREATE DATABASE ${DB} IF NOT EXISTS TOPOLOGY ${WIDE} PRIMARIES 0 SECONDARIES WAIT 300 SECONDS;\"" 2>&1)" \
  || { printf '%s\n' "${create_out}" >&2; dump_state; die "CREATE DATABASE ${DB} failed"; }

deadline=$((SECONDS + TIMEOUT_SECS))
counts=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  counts="$(db_primaries)"
  [[ "${counts}" == "${WIDE} ${WIDE}" ]] && break
  sleep 5
done
[[ "${counts}" == "${WIDE} ${WIDE}" ]] \
  || { dump_state; die "database ${DB}: requested/current primaries '${counts:-not reported}', expected '${WIDE} ${WIDE}' — Neo4j did not allocate the topology the CREATE asked for"; }
log "database ${DB} online at ${WIDE} primaries, the width its creator asked for"
printf 'SHOW SERVERS (hosting) after scale-out:\n%s\n' \
  "$(cypher 'SHOW SERVERS YIELD name,address,state,hosting;')"

# The topology belongs to whoever set it. defaultPrimariesCount is 3 here and this database asks
# for 5, so an operator still aligning databases on that field would pull it back within a couple
# of 15s requeues. Watch long enough to catch that, on both surfaces: the count itself, and the
# absence of any resize Event.
STABLE_SECS="${CLUSTER_SCALE_STABLE_SECONDS:-45}"
log "Holding ${STABLE_SECS}s to prove the operator leaves ${DB} at ${WIDE} primaries (defaultPrimariesCount is a creation default, not a constraint)"
deadline=$((SECONDS + STABLE_SECS))
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  counts="$(db_primaries)"
  [[ "${counts}" == "${WIDE} ${WIDE}" ]] \
    || { dump_state; die "database ${DB} moved to '${counts}' while no pool shrank — the operator rewrote a topology it does not own"; }
  sleep 5
done
early="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
  --field-selector "involvedObject.name=${NEO4J_CR_NAME},reason=${RESIZE_REASON}" \
  -o 'jsonpath={range .items[*]}{.message}{"\n"}{end}' 2>/dev/null | grep -F "${DB}" || true)"
[[ -z "${early}" ]] \
  || { dump_state; die "${RESIZE_REASON} Event for ${DB} before any scale-in: ${early}"; }
log "database ${DB} still at ${WIDE} primaries after ${STABLE_SECS}s, no ${RESIZE_REASON} Event"

# ---------------------------------------------------------------------------
# 4. Scale in — the tail must be drained in Neo4j before the StatefulSet shrinks.
# ---------------------------------------------------------------------------
requested_before="$(db_primaries)"
log "database ${DB} sits at '${requested_before:-unknown}' (requested current) primaries as the scale-in is requested"
scale_members "${NARROW}"

# Observation, not a gate: the drain reasons are catalogued (status/oracle.go) but each pass is a
# 15s requeue, so which one is visible depends on when we look. Failing on a missed sample would
# make this case flaky, so only the outcome below is enforced.
log "Watching for the drain to become visible (ServersPendingDrain)"
deadline=$((SECONDS + 240))
seen_drain=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  if [[ "$(cond ServersPendingDrain status)" == "True" ]]; then
    seen_drain="$(cond ServersPendingDrain reason)"
    log "ServersPendingDrain=True (reason=${seen_drain}) while the StatefulSet still declares $(sts_replicas) member(s) — Neo4j is released first"
    break
  fi
  [[ "$(sts_replicas)" == "${NARROW}" ]] && break
  sleep 2
done
[[ -n "${seen_drain}" ]] || log "drain not sampled (it can complete between two polls) — checking the outcome instead"

wait_members "${NARROW}" "scale-in"

# The gate itself: the operator only lets the StatefulSet shrink after it has written its own
# confirmation that Neo4j released the tail. Missing here would mean the pool shrank around the
# drain, dropping servers that still hosted data.
drain_ok="$(kubectl get "${NEO4J_RESOURCE}" -n "${NEO4J_NAMESPACE}" \
  -o jsonpath='{.status.drainOK.primary}' 2>/dev/null || true)"
[[ "${drain_ok}" == "${NARROW}" ]] \
  || { dump_state; die "status.drainOK.primary='${drain_ok:-absent}', expected ${NARROW} — the StatefulSet shrank without the drain gate (ADD-02)"; }
log "status.drainOK.primary=${drain_ok} — the shrink went through the drain gate"

wait_condition ServersPendingDrain False 120 "scale-in"
wait_settled "scale-in"

# Neo4j's side of the shrink: the survivors are still admitted, the drained ones are not, and the
# tail went one member at a time rather than in one bulk DEALLOCATE.
assert_members_enabled 0 "$((NARROW - 1))" "scale-in"
assert_members_released "${NARROW}" "$((WIDE - 1))" "scale-in"
assert_pool_untouched "${BASELINE_POOL}" "$((NARROW - 1))" "scale-in"
if [[ "$((WIDE - NARROW))" -ge 2 ]]; then
  assert_sequential_drain "$((WIDE - 1))" "$((WIDE - 2))"
fi

# The wide database survived the shrink, narrowed to what the smaller cluster can host.
deadline=$((SECONDS + TIMEOUT_SECS))
counts=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  counts="$(db_primaries)"
  [[ "${counts}" == "${NARROW} ${NARROW}" ]] && break
  sleep 5
done
[[ "${counts}" == "${NARROW} ${NARROW}" ]] \
  || { dump_state; die "database ${DB}: requested/current primaries '${counts:-none}' after the scale-in, expected '${NARROW} ${NARROW}'"; }

# ---------------------------------------------------------------------------
# 5. The narrowing was reported — resizing a cluster resizes databases, never silently.
# ---------------------------------------------------------------------------
assert_resize_reported "${WIDE}" "${NARROW}"

log "Scaled out to ${WIDE} and back in to ${NARROW}: members enabled, tail drained before the shrink, database ${DB} narrowed to ${NARROW} primaries, still online, and the rewrite reported as ${RESIZE_REASON} (AC-NEO-SCALE)"
