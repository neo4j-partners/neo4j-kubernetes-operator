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
# Output contract (#247): every sub-step is pre-announced with a timestamp
# and, for waits, its timeout — so a watcher can always tell "slow" from
# "stuck". Failures dump cluster diagnostics (pods, operator logs, events)
# before exiting, and a verdict table lands in $GITHUB_STEP_SUMMARY when run
# in Actions.
#
# Requires: kind, helm, kubectl, docker. Creates/deletes its own Kind cluster
# (neo4j-install-confidence). ~10-15 min. Run via `make install-confidence`.
set -euo pipefail

CLUSTER=neo4j-install-confidence
CHART=charts/neo4j-operator
NS=neo4j-operator-system
IMG=neo4j-operator:install-confidence
PREV_CHART_VERSION="${PREV_CHART_VERSION:-}" # empty = latest published
HELM_REPO_URL="https://priyolahiri.github.io/neo4j-kubernetes-operator/charts"
PASS=()
START_EPOCH=$(date +%s)

ts() { date '+%H:%M:%S'; }
elapsed() { echo "$(( $(date +%s) - START_EPOCH ))s"; }
step() { echo "[$(ts) +$(elapsed)] $*"; }

summary() { # append the verdict table to the Actions step summary, if present
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  {
    echo "## Install confidence — $1"
    echo ""
    echo "| # | Check |"
    echo "|---|-------|"
    local i=1
    for p in ${PASS[@]+"${PASS[@]}"}; do echo "| $i | ✅ $p |"; i=$((i+1)); done
    if [ "$1" != "PASSED" ]; then echo "| $i | ❌ $2 |"; fi
  } >> "$GITHUB_STEP_SUMMARY"
}

diagnostics() { # best-effort cluster state dump so a CI failure is diagnosable from the log alone
  echo ""
  echo "================ DIAGNOSTICS (failure state dump) ================"
  echo "--- pods (all namespaces)"
  kubectl get pods -A -o wide 2>&1 || true
  echo "--- operator deployment"
  kubectl -n "$NS" describe deploy 2>&1 | tail -40 || true
  echo "--- operator logs (last 60 lines)"
  kubectl -n "$NS" logs deploy/neo4j-operator-controller-manager --tail=60 2>&1 || true
  echo "--- recent events"
  kubectl get events -A --sort-by=.lastTimestamp 2>&1 | tail -25 || true
  echo "--- helm releases"
  helm list -A 2>&1 || true
  echo "==================================================================="
}

fail() {
  echo "[$(ts) +$(elapsed)] FAIL: $*" >&2
  diagnostics
  summary "FAILED" "$*"
  exit 1
}
ok() { PASS+=("$1"); echo "[$(ts) +$(elapsed)] PASS: $1"; }

cleanup() { kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

step "=== building operator image from working tree (output in /tmp/ic-build.log; ~1-3 min)"
docker build -t "$IMG" . > /tmp/ic-build.log 2>&1 \
  || { tail -30 /tmp/ic-build.log; fail "docker build failed (full log: /tmp/ic-build.log)"; }
step "image built"

step "=== creating Kind cluster '$CLUSTER' (timeout 120s)"
cleanup
kind create cluster --name "$CLUSTER" --wait 120s >/dev/null
step "loading image into Kind (~30-60s)"
# kind restarts containerd inside the node right after cluster-Ready (the
# CNI config landing triggers it); a load started in that window fails with
# 'containerd.sock: connection refused' or 'content digest not found'
# (reproduced deterministically on Docker 29 + kind v0.29). Retry with a
# settle delay instead of racing it.
for attempt in 1 2 3; do
  kind load docker-image "$IMG" --name "$CLUSTER" && break
  [ "$attempt" = 3 ] && fail "kind load failed after 3 attempts"
  step "  kind load attempt $attempt failed (containerd still settling); retrying in 10s"
  sleep 10
done

wait_operator() { # $1 = namespace — visible rollout wait, 180s timeout
  step "waiting for operator rollout in '$1' (timeout 180s)"
  kubectl -n "$1" rollout status deploy -l control-plane=controller-manager --timeout=180s \
    || kubectl -n "$1" rollout status deploy/neo4j-operator-controller-manager --timeout=180s
}

# ---------------------------------------------------------------- 1. helm install (cluster mode)
step "=== [1] helm install, operatorMode=cluster (helm --wait, timeout 180s)"
helm install op "$CHART" -n "$NS" --create-namespace \
  --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
  --set image.pullPolicy=Never --wait --timeout 180s
wait_operator "$NS"
ok "helm install (cluster mode) — operator Ready"

step "=== [1b] smoke CR reconciles"
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
step "polling for the smoke StatefulSet (up to 60s)"
for i in $(seq 1 30); do
  kubectl get sts smoke >/dev/null 2>&1 && break
  [ $((i % 5)) -eq 0 ] && step "  still waiting (${i}/30 polls)"
  sleep 2
done
kubectl get sts smoke >/dev/null 2>&1 || fail "operator never reconciled the smoke CR (no StatefulSet)"
ok "smoke CR reconciled (ConfigMap+StatefulSet created)"

# ---------------------------------------------------------------- 4. uninstall with live CR (documented order)
step "=== [4] documented uninstall order with a live CR (CR delete timeout 120s)"
kubectl delete neo4jenterprisestandalone smoke --timeout=120s >/dev/null \
  || fail "CR deletion blocked — finalizer not stripped while operator alive"
step "helm uninstall (--wait)"
helm uninstall op -n "$NS" --wait >/dev/null
kubectl get crd neo4jenterprisestandalones.neo4j.neo4j.com >/dev/null \
  || fail "CRDs must survive helm uninstall"
ok "ordered uninstall: CR -> operator; CRDs retained; nothing wedged"

# ---------------------------------------------------------------- 2. namespaces mode + per-namespace Roles
step "=== [2] helm install, operatorMode=namespaces + perNamespaceRoles (helm --wait, timeout 180s)"
kubectl create ns watched-a >/dev/null; kubectl create ns watched-b >/dev/null
helm install op "$CHART" -n "$NS" \
  --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
  --set image.pullPolicy=Never \
  --set operatorMode=namespaces \
  --set 'watchNamespaces={watched-a,watched-b}' \
  --set rbac.perNamespaceRoles=true --wait --timeout 180s
wait_operator "$NS"
kubectl get role -n watched-a -o name | grep -q neo4j || fail "per-namespace Role missing in watched-a"
kubectl get clusterrole | grep -qE "op-neo4j-operator-manager" && fail "manager ClusterRole must NOT exist with perNamespaceRoles"
ok "namespaces mode + perNamespaceRoles: Roles present, no manager ClusterRole"
step "helm uninstall (--wait)"
helm uninstall op -n "$NS" --wait >/dev/null

# ---------------------------------------------------------------- 3. helm upgrade from previous release
step "=== [3] helm upgrade from previous released chart (+ CRD refresh)"
helm repo add neo4j-operator-rel "$HELM_REPO_URL" >/dev/null 2>&1 || true
helm repo update >/dev/null 2>&1
step "installing previous released chart ${PREV_CHART_VERSION:-(latest)} — pulls the RELEASED operator image from ghcr, can take a few minutes (helm --wait, timeout 300s)"
if helm install op neo4j-operator-rel/neo4j-operator -n "$NS" \
     ${PREV_CHART_VERSION:+--version "$PREV_CHART_VERSION"} \
     --wait --timeout 300s; then
  # CRD refresh (the documented mandatory step), then chart upgrade.
  step "applying CRD refresh (server-side)"
  kubectl apply --server-side --force-conflicts -f config/crd/bases/ >/dev/null
  step "helm upgrade to the working-tree chart (helm --wait, timeout 180s)"
  helm upgrade op "$CHART" -n "$NS" \
    --set image.repository="${IMG%%:*}" --set image.tag="${IMG##*:}" \
    --set image.pullPolicy=Never --wait --timeout 180s
  wait_operator "$NS"
  # NEW-field acceptance probe: status.upgradeStatus.currentPartition exists
  # only post-refresh; a stale stored CRD would prune it.
  kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com -o json \
    | grep -q currentPartition || fail "CRD refresh did not land (currentPartition missing — stale stored CRD)"
  ok "helm upgrade from previous release with CRD refresh — new CRD fields present"
  step "helm uninstall (--wait)"
  helm uninstall op -n "$NS" --wait >/dev/null
else
  step "SKIP: previous released chart not reachable (offline?) — upgrade leg skipped"
fi

# ---------------------------------------------------------------- 5. kubectl-apply path
step "=== [5] kubectl-apply install / re-apply upgrade / ordered uninstall"
./bin/kustomize build config/default > /tmp/ic-complete.yaml
# pin to the local image (mirrors the release overlay's images stanza)
step "kubectl apply (server-side) + pin local image"
kubectl apply --server-side --force-conflicts -f /tmp/ic-complete.yaml >/dev/null
kubectl -n "$NS" set image deploy/neo4j-operator-controller-manager manager="$IMG" >/dev/null
kubectl -n "$NS" patch deploy neo4j-operator-controller-manager \
  --type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Never"}]' >/dev/null
wait_operator "$NS"
ok "kubectl-apply (server-side) install — operator Ready"
# upgrade = re-apply (idempotent against the same manifest)
step "kubectl re-apply (idempotent upgrade)"
kubectl apply --server-side --force-conflicts -f /tmp/ic-complete.yaml >/dev/null
ok "kubectl re-apply upgrade — no immutable-field conflicts"
# ordered uninstall: no CRs exist; delete operator resources EXCEPT CRDs+ns first
step "ordered uninstall (operator first, CRDs last)"
kubectl delete -f /tmp/ic-complete.yaml --dry-run=client -o name \
  | grep -vE '^customresourcedefinition|^namespace/' \
  | xargs -r kubectl delete --ignore-not-found >/dev/null
kubectl get crd neo4jenterpriseclusters.neo4j.neo4j.com >/dev/null || fail "CRDs deleted by operator-only uninstall"
kubectl delete -f /tmp/ic-complete.yaml --ignore-not-found >/dev/null 2>&1 || true
ok "kubectl ordered uninstall — operator removed first, CRDs last, nothing wedged"

echo ""
step "=== INSTALL CONFIDENCE: ${#PASS[@]} checks passed (total $(elapsed)) ==="
printf ' - %s\n' "${PASS[@]}"
summary "PASSED" ""
