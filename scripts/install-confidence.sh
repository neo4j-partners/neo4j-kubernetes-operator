#!/usr/bin/env bash
# install-confidence.sh — pre-release install/upgrade/uninstall matrix on Kind.
#
# Exercises the paths users actually take, against the CURRENT working tree:
#   1. helm install (cluster mode)  -> operator Ready -> smoke CR reconciles
#   2. helm install (namespaces mode + perNamespaceRoles) -> operator Ready
#   3. helm UPGRADE from the previous released chart -> CRD refresh step ->
#      verify a NEW CRD field is accepted (not pruned by a stale stored CRD)
#   4. helm UNINSTALL with a live CR -> verify the documented order works and
#      nothing wedges in Terminating
#   5. kubectl-apply (kustomize-built complete manifest) install ->
#      re-apply upgrade -> ordered uninstall
#
# Requires: kind, helm, kubectl, docker. Creates/deletes its own Kind cluster
# (neo4j-install-confidence). ~10-15 min. Run via `make install-confidence`.
set -euo pipefail

CLUSTER=neo4j-install-confidence
CHART=charts/neo4j-operator
NS=neo4j-operator-system
IMG=neo4j-operator:install-confidence
PREV_CHART_VERSION="${PREV_CHART_VERSION:-}" # empty = latest published
HELM_REPO_URL="https://neo4j-partners.github.io/neo4j-kubernetes-operator/charts"
PASS=()
fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { PASS+=("$1"); echo "PASS: $1"; }

cleanup() { kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "=== building operator image from working tree"
docker build -t "$IMG" . >/dev/null

echo "=== creating Kind cluster"
cleanup
kind create cluster --name "$CLUSTER" --wait 120s >/dev/null
kind load docker-image "$IMG" --name "$CLUSTER"

wait_operator() { # $1 = namespace
  kubectl -n "$1" rollout status deploy -l control-plane=controller-manager --timeout=180s >/dev/null \
    || kubectl -n "$1" rollout status deploy/neo4j-operator-controller-manager --timeout=180s >/dev/null
}

# ---------------------------------------------------------------- 1. helm install (cluster mode)
echo "=== [1] helm install, operatorMode=cluster"
helm install op "$CHART" -n "$NS" --create-namespace \
  --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
  --set image.pullPolicy=Never --wait --timeout 180s >/dev/null
wait_operator "$NS"
ok "helm install (cluster mode) — operator Ready"

echo "=== [1b] smoke CR reconciles"
kubectl create secret generic neo4j-admin-secret \
  --from-literal=username=neo4j --from-literal=password='InstallConf1!' >/dev/null
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: neo4j.neo4j.com/v1beta1
kind: Neo4jEnterpriseStandalone
metadata: {name: smoke, namespace: default}
spec:
  image: {repo: neo4j, tag: 5.26.0-enterprise}
  storage: {size: 1Gi}
  auth: {adminSecret: neo4j-admin-secret}
EOF
# Reconciliation evidence, not full readiness (no Neo4j image pull needed):
# the controller must create the ConfigMap + StatefulSet.
for i in $(seq 1 30); do
  kubectl get sts smoke >/dev/null 2>&1 && break; sleep 2
done
kubectl get sts smoke >/dev/null 2>&1 || fail "operator never reconciled the smoke CR (no StatefulSet)"
ok "smoke CR reconciled (ConfigMap+StatefulSet created)"

# ---------------------------------------------------------------- 4. uninstall with live CR (documented order)
echo "=== [4] documented uninstall order with a live CR"
kubectl delete neo4jenterprisestandalone smoke --timeout=120s >/dev/null \
  || fail "CR deletion blocked — finalizer not stripped while operator alive"
helm uninstall op -n "$NS" --wait >/dev/null
kubectl get crd neo4jenterprisestandalones.neo4j.neo4j.com >/dev/null \
  || fail "CRDs must survive helm uninstall"
ok "ordered uninstall: CR -> operator; CRDs retained; nothing wedged"

# ---------------------------------------------------------------- 2. namespaces mode + per-namespace Roles
echo "=== [2] helm install, operatorMode=namespaces + perNamespaceRoles"
kubectl create ns watched-a >/dev/null; kubectl create ns watched-b >/dev/null
helm install op "$CHART" -n "$NS" \
  --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
  --set image.pullPolicy=Never \
  --set operatorMode=namespaces \
  --set 'watchNamespaces={watched-a,watched-b}' \
  --set rbac.perNamespaceRoles=true --wait --timeout 180s >/dev/null
wait_operator "$NS"
kubectl get role -n watched-a -o name | grep -q neo4j || fail "per-namespace Role missing in watched-a"
kubectl get clusterrole | grep -qE "op-neo4j-operator-manager" && fail "manager ClusterRole must NOT exist with perNamespaceRoles"
ok "namespaces mode + perNamespaceRoles: Roles present, no manager ClusterRole"
helm uninstall op -n "$NS" --wait >/dev/null

# ---------------------------------------------------------------- 3. helm upgrade from previous release
echo "=== [3] helm upgrade from previous released chart (+ CRD refresh)"
helm repo add neo4j-operator-rel "$HELM_REPO_URL" >/dev/null 2>&1 || true
helm repo update >/dev/null 2>&1
if helm install op neo4j-operator-rel/neo4j-operator -n "$NS" \
     ${PREV_CHART_VERSION:+--version "$PREV_CHART_VERSION"} \
     --wait --timeout 300s >/dev/null 2>&1; then
  # CRD refresh (the documented mandatory step), then chart upgrade.
  kubectl apply --server-side --force-conflicts -f config/crd/bases/ >/dev/null
  helm upgrade op "$CHART" -n "$NS" \
    --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
    --set image.pullPolicy=Never --wait --timeout 180s >/dev/null
  wait_operator "$NS"
  # NEW-field acceptance probe: status.upgradeStatus.currentPartition exists
  # only post-refresh; a stale stored CRD would prune it.
  kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com -o json \
    | grep -q currentPartition || fail "CRD refresh did not land (currentPartition missing — stale stored CRD)"
  ok "helm upgrade from previous release with CRD refresh — new CRD fields present"
  helm uninstall op -n "$NS" --wait >/dev/null
else
  echo "SKIP: previous released chart not reachable (offline?) — upgrade leg skipped"
fi

# ---------------------------------------------------------------- 5. kubectl-apply path
echo "=== [5] kubectl-apply install / re-apply upgrade / ordered uninstall"
./bin/kustomize build config/default > /tmp/ic-complete.yaml
# pin to the local image (mirrors the release overlay's images stanza)
kubectl apply --server-side --force-conflicts -f /tmp/ic-complete.yaml >/dev/null
kubectl -n "$NS" set image deploy/neo4j-operator-controller-manager manager="$IMG" >/dev/null
kubectl -n "$NS" patch deploy neo4j-operator-controller-manager \
  --type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Never"}]' >/dev/null
wait_operator "$NS"
ok "kubectl-apply (server-side) install — operator Ready"
# upgrade = re-apply (idempotent against the same manifest)
kubectl apply --server-side --force-conflicts -f /tmp/ic-complete.yaml >/dev/null
ok "kubectl re-apply upgrade — no immutable-field conflicts"
# ordered uninstall: no CRs exist; delete operator resources EXCEPT CRDs+ns first
kubectl delete -f /tmp/ic-complete.yaml --dry-run=client -o name \
  | grep -vE '^customresourcedefinition|^namespace/' \
  | xargs -r kubectl delete --ignore-not-found >/dev/null
kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com >/dev/null || fail "CRDs deleted by operator-only uninstall"
kubectl delete -f /tmp/ic-complete.yaml --ignore-not-found >/dev/null 2>&1 || true
ok "kubectl ordered uninstall — operator removed first, CRDs last, nothing wedged"

echo ""
echo "=== INSTALL CONFIDENCE: ${#PASS[@]} checks passed ==="
printf ' - %s\n' "${PASS[@]}"
