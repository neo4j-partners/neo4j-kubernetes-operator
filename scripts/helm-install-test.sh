#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "${SCRIPT_DIR}")"

RELEASE_NAME="${RELEASE_NAME:-neo4j-operator}"
NAMESPACE="${NAMESPACE:-neo4j-operator-system}"
CHART_DIR="${CHART_DIR:-${PROJECT_ROOT}/charts/neo4j-operator}"
KIND_CLUSTER="${KIND_CLUSTER:-neo4j-operator-test}"
SKIP_CLEANUP="${SKIP_CLEANUP:-false}"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

cleanup() {
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log "Skipping cleanup (SKIP_CLEANUP=true)"
        return
    fi

    log "Cleaning up Helm release..."
    helm uninstall "${RELEASE_NAME}" --namespace "${NAMESPACE}" >/dev/null 2>&1 || true
    kubectl delete namespace "${NAMESPACE}" --ignore-not-found=true --timeout=120s >/dev/null 2>&1 || true
}

trap cleanup EXIT

for cmd in helm kubectl kind; do
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        echo "Required command not found: ${cmd}" >&2
        exit 1
    fi
done

if ! kind get clusters | grep -q "^${KIND_CLUSTER}$"; then
    log "Kind cluster ${KIND_CLUSTER} not found; creating..."
    make test-cluster
fi

log "Switching to Kind cluster ${KIND_CLUSTER}"
kind export kubeconfig --name "${KIND_CLUSTER}"

log "Linting Helm chart"
helm lint "${CHART_DIR}"

# --- RBAC render assertions (no cluster needed) -----------------------------
# Locks the operatorMode / rbac.perNamespaceRoles RBAC matrix (#206) so a chart
# edit can't silently regress which Role/ClusterRole objects get emitted. Pure
# `helm template` — runs locally; not wired into per-PR CI.
log "Verifying RBAC render matrix"
render_tpl() { # <template-file> <extra --set args...>
    local tpl="$1"; shift
    helm template "${RELEASE_NAME}" "${CHART_DIR}" --namespace "${NAMESPACE}" \
        --show-only "templates/${tpl}" "$@" 2>/dev/null
}
assert_has()    { grep -q "$2" <<<"$1" || { echo "RBAC assert FAILED: expected '$3'"; exit 1; }; }
assert_absent() { if grep -q "$2" <<<"$1"; then echo "RBAC assert FAILED: did not expect '$3'"; exit 1; fi; }

# Default (cluster scope): manager ClusterRole present, no per-namespace Roles.
assert_has    "$(render_tpl clusterrole.yaml)"      'kind: ClusterRole' "manager ClusterRole (cluster mode)"
assert_absent "$(render_tpl namespaced-roles.yaml)" 'kind: Role'        "per-namespace Role (cluster mode)"

# namespaces + perNamespaceRoles=false (default): manager ClusterRole present.
assert_has "$(render_tpl clusterrole.yaml --set operatorMode=namespaces --set 'watchNamespaces={team-a,team-b}')" \
    'kind: ClusterRole' "manager ClusterRole (namespaces, perNamespaceRoles off)"

# namespaces + perNamespaceRoles=true + static list: per-namespace Roles, NO manager ClusterRole.
NS_ARGS=(--set operatorMode=namespaces --set rbac.perNamespaceRoles=true --set 'watchNamespaces={team-a,team-b}')
assert_absent "$(render_tpl clusterrole.yaml "${NS_ARGS[@]}")" 'kind: ClusterRole' "manager ClusterRole (perNamespaceRoles=true)"
out_ns="$(render_tpl namespaced-roles.yaml "${NS_ARGS[@]}")"
assert_has "$out_ns" 'namespace: team-a' "manager Role in team-a"
assert_has "$out_ns" 'namespace: team-b' "manager Role in team-b"
assert_has "$out_ns" 'kind: RoleBinding'  "per-namespace RoleBinding"
# metrics ClusterRoles must survive (authn/authz APIs are cluster-scoped).
assert_has "$(render_tpl metrics-rbac.yaml "${NS_ARGS[@]}" --set metrics.secure=true)" \
    'kind: ClusterRole' "metrics ClusterRole retained"

# perNamespaceRoles=true + a pattern → must fail fast.
if helm template "${RELEASE_NAME}" "${CHART_DIR}" --namespace "${NAMESPACE}" \
    --set operatorMode=namespaces --set rbac.perNamespaceRoles=true --set 'watchNamespaces={team-*}' >/dev/null 2>&1; then
    echo "RBAC assert FAILED: pattern + perNamespaceRoles=true should fail render"; exit 1
fi
# perNamespaceRoles=true + empty list → must fail fast.
if helm template "${RELEASE_NAME}" "${CHART_DIR}" --namespace "${NAMESPACE}" \
    --set operatorMode=namespaces --set rbac.perNamespaceRoles=true >/dev/null 2>&1; then
    echo "RBAC assert FAILED: empty watchNamespaces + perNamespaceRoles=true should fail render"; exit 1
fi
log "RBAC render matrix verified"

log "Installing Helm chart"
helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" \
    --namespace "${NAMESPACE}" \
    --create-namespace

log "Waiting for operator deployment"
kubectl wait --for=condition=available deployment \
    -l app.kubernetes.io/instance="${RELEASE_NAME}" \
    -n "${NAMESPACE}" \
    --timeout=300s

log "Verifying CRDs"
crds=(
    neo4jbackups.neo4j.neo4j.com
    neo4jdatabases.neo4j.neo4j.com
    neo4jenterpriseclusters.neo4j.neo4j.com
    neo4jenterprisestandalones.neo4j.neo4j.com
    neo4jplugins.neo4j.neo4j.com
    neo4jrestores.neo4j.neo4j.com
    neo4jshardeddatabases.neo4j.neo4j.com
)

for crd in "${crds[@]}"; do
    kubectl get crd "${crd}" >/dev/null
    log "Found CRD: ${crd}"
done

log "Helm chart installation verified"
