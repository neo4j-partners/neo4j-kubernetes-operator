#!/usr/bin/env bash
# Delete the EKS nodegroups and the cluster. The ECR repository is kept on purpose.
#
# A script rather than an inline step, unlike the Azure and GCP teardowns, because the order is
# not optional and the wait needs a bound: EKS refuses to delete a cluster that still owns a
# nodegroup, and `aws eks wait` gives up only after 40 minutes — long enough to hold a runner for
# nothing. Shared by the e2e job's teardown step and by cloud-cleanup.yml.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config aws-eks

require_cmd aws

export AWS_REGION
export AWS_DEFAULT_REGION="${AWS_REGION}"

# Space-separated nodegroup names, empty when there are none. `--output text` answers an empty list
# with either nothing or the literal None depending on the query, and None read as a name would keep
# the wait below spinning on a nodegroup that does not exist.
_list_nodegroups() {
  aws eks list-nodegroups --cluster-name "${AWS_EKS_NAME}" \
    --query 'nodegroups[]' --output text 2>/dev/null \
    | tr '\t' '\n' | grep -v '^None$' | tr '\n' ' ' || true
}

timeout_seconds="${AWS_EKS_TEARDOWN_TIMEOUT}"

if ! aws eks describe-cluster --name "${AWS_EKS_NAME}" >/dev/null 2>&1; then
  log "EKS cluster ${AWS_EKS_NAME} not found — nothing to tear down"
  exit 0
fi

# Deleting a managed nodegroup does not terminate its instances outright: EKS drains each node
# through the eviction API and waits for the pods to go. A Neo4j pod asks for an hour of grace
# (render/workload/scheduling.go) and a PodDisruptionBudget can refuse eviction altogether, so the
# drain would sit there long past any bound worth holding a runner for. Nothing here needs a graceful
# shutdown — the cluster is being destroyed — so clear both obstacles first. Best-effort throughout:
# a cluster we cannot reach with kubectl is exactly a cluster whose nodegroup we still want gone.
if command -v kubectl >/dev/null 2>&1 \
  && aws eks update-kubeconfig --name "${AWS_EKS_NAME}" >/dev/null 2>&1; then
  log "Clearing what would block the node drain"
  # PDBs first: while one stands, eviction of the pods it covers is refused outright.
  kubectl delete poddisruptionbudget --all --all-namespaces --ignore-not-found \
    --timeout=60s >/dev/null 2>&1 || true
  # Then the workloads, so the controllers do not put the pods back while the drain removes them.
  kubectl delete statefulset,deployment --all --all-namespaces --ignore-not-found \
    --cascade=foreground --timeout=120s >/dev/null 2>&1 || true
  # Whatever is left goes now rather than in an hour. --force skips the graceful period entirely.
  kubectl delete pods --all --all-namespaces --force --grace-period=0 \
    --ignore-not-found --timeout=120s >/dev/null 2>&1 || true
else
  log "kubectl cannot reach ${AWS_EKS_NAME} — deleting the nodegroup without draining it first"
fi

# Every nodegroup, not only the one CI creates: a cluster that outlived a run may carry one under
# an older name, and delete-cluster fails while any of them remains. One already DELETING is left
# alone — a second delete call on it is refused, and this script is meant to be safe to re-run on a
# teardown that was interrupted, which is the common case after a cancelled job.
for nodegroup in $(_list_nodegroups); do
  state="$(aws eks describe-nodegroup --cluster-name "${AWS_EKS_NAME}" \
    --nodegroup-name "${nodegroup}" --query 'nodegroup.status' --output text 2>/dev/null || true)"
  if [[ "${state}" == "DELETING" ]]; then
    log "Nodegroup ${nodegroup} is already DELETING — the eviction blockers cleared above should let it finish"
    continue
  fi
  log "Deleting nodegroup ${nodegroup} (${state:-unknown})"
  aws eks delete-nodegroup --cluster-name "${AWS_EKS_NAME}" \
    --nodegroup-name "${nodegroup}" >/dev/null
done

# Report each nodegroup's state, not just how many are left: DELETING and DELETE_FAILED both count
# as one, and only the second means waiting is pointless. health.issues is where EKS explains a
# drain that cannot finish.
deadline=$((SECONDS + timeout_seconds))
while (( SECONDS < deadline )); do
  remaining="$(_list_nodegroups)"
  [[ -n "${remaining// /}" ]] || break
  for nodegroup in ${remaining}; do
    state="$(aws eks describe-nodegroup --cluster-name "${AWS_EKS_NAME}" \
      --nodegroup-name "${nodegroup}" --query 'nodegroup.status' --output text 2>/dev/null || true)"
    log "Nodegroup ${nodegroup} is ${state:-gone}"
    if [[ "${state}" == "DELETE_FAILED" ]]; then
      aws eks describe-nodegroup --cluster-name "${AWS_EKS_NAME}" \
        --nodegroup-name "${nodegroup}" --query 'nodegroup.health.issues' --output json || true
      die "nodegroup ${nodegroup} is DELETE_FAILED, so waiting changes nothing. Delete its Auto Scaling group in the EC2 console, then re-run the cloud-cleanup workflow — the control plane bills by the hour until the cluster is gone"
    fi
  done
  sleep 20
done

leftover="$(_list_nodegroups)"
if [[ -n "${leftover// /}" ]]; then
  log "WARNING: ${leftover}still present after ${timeout_seconds}s — delete-cluster below will refuse"
fi

log "Deleting cluster ${AWS_EKS_NAME}"
# No wait afterwards: the control plane deletion is asynchronous, like `az group delete --no-wait`
# and `gcloud container clusters delete --async` in the other two teardowns.
if ! aws eks delete-cluster --name "${AWS_EKS_NAME}" >/dev/null; then
  die "delete-cluster failed for ${AWS_EKS_NAME}, most likely a nodegroup still deleting. Re-run the cloud-cleanup workflow — the control plane bills by the hour until it is gone"
fi

log "Deletion of ${AWS_EKS_NAME} started"
