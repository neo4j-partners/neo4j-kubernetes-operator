#!/usr/bin/env bash
# Storage assertion helpers (BDR-005 / render/storage).
#
# Shared by the p4-storage asserts: wait for the operator to reconcile / Neo4j to be
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
