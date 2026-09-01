#!/usr/bin/env bash
# Storage assertion helpers (BDR-005 / render/storage).
#
# Shared by the feature-storage asserts: wait for the operator to reconcile / Neo4j to be
# Ready, verify a mount is actually present inside the neo4j container (via /proc/mounts,
# which needs no write permission), and confirm the expected failure surface for
# invalid storage (PVC/pod stuck Pending, CR never Ready).

# Standalone pod hosting the neo4j container.
storage_pod() { printf '%s-0' "${NEO4J_STS_NAME}"; }

# Dynamic / volumeClaimTemplate data PVC name (VCT "data" on the StatefulSet).
storage_data_pvc() { printf 'data-%s-0' "${NEO4J_STS_NAME}"; }

# storage_wait_installed [timeout] — operator reconciled operands (Installed=True).
storage_wait_installed() {
  local timeout="${1:-${E2E_ASSERT_TIMEOUT:-300s}}"
  local res="neo4j/${NEO4J_CR_NAME}"
  log "Waiting for ${res} Installed condition (timeout ${timeout})"
  if ! kubectl wait --for=condition=Installed "${res}" \
    -n "${NEO4J_NAMESPACE}" --timeout="${timeout}" 2>/dev/null; then
    kubectl describe "${res}" -n "${NEO4J_NAMESPACE}" >&2 || true
    die "${res} Installed condition not True within ${timeout}"
  fi
}

# storage_wait_ready [timeout] — Neo4j accepts connections (Ready=True + Running pod).
storage_wait_ready() {
  local timeout="${1:-600s}"
  local res="neo4j/${NEO4J_CR_NAME}"
  storage_wait_installed
  log "Waiting for ${res} Ready condition (timeout ${timeout})"
  if ! kubectl wait --for=condition=Ready "${res}" \
    -n "${NEO4J_NAMESPACE}" --timeout="${timeout}" 2>/dev/null; then
    kubectl describe "${res}" -n "${NEO4J_NAMESPACE}" >&2 || true
    kubectl get pods,pvc -n "${NEO4J_NAMESPACE}" \
      -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" -o wide >&2 || true
    die "${res} Ready condition not True within ${timeout}"
  fi
  log "${res} Ready"
}

# storage_assert_mountpoint <path> [label] — fail unless <path> is a real mount inside
# the neo4j container. Reads /proc/mounts (field 2 = mountpoint) so it works regardless
# of directory ownership/permissions (the neo4j user cannot always write into a mount).
storage_assert_mountpoint() {
  local path=$1 label="${2:-mount}"
  local pod
  pod="$(storage_pod)"
  if kubectl exec -n "${NEO4J_NAMESPACE}" "${pod}" -c neo4j -- \
    awk -v p="${path}" '$2==p{found=1} END{exit found?0:1}' /proc/mounts 2>/dev/null; then
    log "[${label}] ${path} is a mount point inside the neo4j container"
  else
    kubectl exec -n "${NEO4J_NAMESPACE}" "${pod}" -c neo4j -- \
      sh -c 'grep " /data\| /logs\| /metrics\| /mnt" /proc/mounts || cat /proc/mounts' >&2 2>/dev/null || true
    die "[${label}] ${path} is not mounted inside the neo4j container"
  fi
}

# storage_pvc_phase <pvc> — echo a PVC's phase (Pending/Bound) or empty if absent.
storage_pvc_phase() {
  kubectl get pvc "$1" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || printf ''
}

# --- Volume expansion (BDR-005) -------------------------------------------------------------
#
# A grow is applied to the claims, never to the StatefulSet's volumeClaimTemplates, which
# Kubernetes makes immutable once the StatefulSet exists. So these helpers read PVCs, and the
# template is only ever checked to confirm it did NOT move.

# storage_condition <type> <field> — one field of one CR condition, empty when absent.
storage_condition() {
  kubectl get "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || printf ''
}

# storage_claims — every operator-owned claim of this CR, one name per line. The set is
# discovered rather than derived, so the same assert serves a Standalone and every pool of a
# Cluster without being told the topology.
storage_claims() {
  kubectl get pvc -n "${NEO4J_NAMESPACE}" --no-headers -o custom-columns='NAME:.metadata.name' \
    -l "app.kubernetes.io/instance=${NEO4J_CR_NAME},neo4j.com/component=storage" \
    2>/dev/null | sed '/^$/d'
}

# storage_claim_field <pvc> <requested|actual> — the size a claim asks for, or the size it has.
storage_claim_field() {
  local path='{.spec.resources.requests.storage}'
  [[ "$2" == actual ]] && path='{.status.capacity.storage}'
  kubectl get pvc "$1" -n "${NEO4J_NAMESPACE}" -o jsonpath="${path}" 2>/dev/null || printf ''
}

# storage_claim_table — "name requested actual" per claim, for logs and failure dumps.
storage_claim_table() {
  local pvc
  while read -r pvc; do
    [[ -n "${pvc}" ]] || continue
    printf '  %-42s requested=%-8s actual=%s\n' \
      "${pvc}" "$(storage_claim_field "${pvc}" requested)" "$(storage_claim_field "${pvc}" actual)"
  done <<<"$(storage_claims)"
}

# storage_template_sizes — "statefulset=size" per pool StatefulSet.
storage_template_sizes() {
  kubectl get sts -n "${NEO4J_NAMESPACE}" -l "app.kubernetes.io/instance=${NEO4J_CR_NAME}" \
    -o jsonpath='{range .items[*]}{.metadata.name}={.spec.volumeClaimTemplates[0].spec.resources.requests.storage} {end}' \
    2>/dev/null || printf ''
}

# storage_data_class — the StorageClass the data claim is actually bound through, which is what
# the API server consults when the operator asks to grow it.
storage_data_class() {
  local pvc
  pvc="$(storage_claims | head -1)"
  [[ -n "${pvc}" ]] || return 0
  kubectl get pvc "${pvc}" -n "${NEO4J_NAMESPACE}" -o jsonpath='{.spec.storageClassName}' 2>/dev/null || printf ''
}

storage_class_provisioner() {
  kubectl get storageclass "$1" -o jsonpath='{.provisioner}' 2>/dev/null || printf ''
}

storage_class_expansion() {
  kubectl get storageclass "$1" -o jsonpath='{.allowVolumeExpansion}' 2>/dev/null || printf ''
}

# storage_set_class_expansion <class> <true|false> — allowVolumeExpansion is a mutable field, and
# flipping it is the only portable way to exercise both outcomes: kind's local-path class forbids
# expansion while every managed provider's default class allows it. Callers restore it.
storage_set_class_expansion() {
  local class=$1 want=$2
  kubectl patch storageclass "${class}" --type merge \
    -p "{\"allowVolumeExpansion\":${want}}" >/dev/null 2>&1 \
    || die "cannot set allowVolumeExpansion=${want} on StorageClass ${class}"
  log "StorageClass ${class}: allowVolumeExpansion=${want}"
}

# storage_simulate_resizer <pvc> <size> — write a claim's status.capacity by hand.
#
# Only for a provisioner with no resizer behind it. kind's rancher.io/local-path accepts the
# larger request (once the class allows expansion) but nothing ever updates the capacity, so the
# claim would sit half-grown forever and the completion path would be untestable locally. On a
# real CSI driver this is never called and the capacity is earned.
storage_simulate_resizer() {
  local pvc=$1 size=$2
  kubectl patch pvc "${pvc}" -n "${NEO4J_NAMESPACE}" --subresource=status --type merge \
    -p "{\"status\":{\"capacity\":{\"storage\":\"${size}\"}}}" >/dev/null \
    || die "cannot write status.capacity on ${pvc} to stand in for a resizer"
}

# storage_event_count <reason> — how many times the operator recorded that reason on this CR.
# Counts matter here: the completion Event is edge-triggered and must land exactly once.
#
# Selected on the object's UID, not its name. Events outlive the object they describe (an hour by
# default), so a CR deleted and recreated under the same name — which every local rerun of a case
# does — would inherit its predecessor's Events and be credited with a resize it never performed.
storage_event_count() {
  local uid total
  uid="$(kubectl get "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" \
    -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  [[ -n "${uid}" ]] || { printf '0'; return; }
  total="$(kubectl get events -n "${NEO4J_NAMESPACE}" \
    --field-selector "involvedObject.uid=${uid},reason=$1" \
    -o jsonpath='{range .items[*]}{.count}{"\n"}{end}' 2>/dev/null \
    | awk '{s += ($1 == "" ? 1 : $1)} END {print s + 0}')"
  printf '%s' "${total:-0}"
}

# storage_dump <label> — everything needed to read a storage failure without a rerun.
storage_dump() {
  log "[$1] storage state:"
  storage_claim_table >&2
  printf '  templates: %s\n' "$(storage_template_sizes)" >&2
  printf '  StorageReady=%s/%s  %s\n' \
    "$(storage_condition StorageReady status)" "$(storage_condition StorageReady reason)" \
    "$(storage_condition StorageReady message)" >&2
  printf '  Ready=%s/%s\n' \
    "$(storage_condition Ready status)" "$(storage_condition Ready reason)" >&2
  kubectl get events -n "${NEO4J_NAMESPACE}" \
    --field-selector "involvedObject.name=${NEO4J_CR_NAME}" \
    -o custom-columns='COUNT:.count,TYPE:.type,REASON:.reason,MESSAGE:.message' --no-headers \
    2>/dev/null | grep -i -E 'storage|resize' >&2 || true
}

# storage_patch_size <size> — ask the CR for a new data volume size.
storage_patch_size() {
  kubectl patch "neo4j/${NEO4J_CR_NAME}" -n "${NEO4J_NAMESPACE}" --type merge \
    -p "{\"spec\":{\"storage\":{\"volumes\":{\"data\":{\"dynamic\":{\"size\":\"$1\"}}}}}}" 2>&1
}
