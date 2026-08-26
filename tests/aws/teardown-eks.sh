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

timeout_seconds="${AWS_EKS_TEARDOWN_TIMEOUT}"

if ! aws eks describe-cluster --name "${AWS_EKS_NAME}" >/dev/null 2>&1; then
  log "EKS cluster ${AWS_EKS_NAME} not found — nothing to tear down"
  exit 0
fi

# Every nodegroup, not only the one CI creates: a cluster that outlived a run may carry one under
# an older name, and delete-cluster fails while any of them remains.
while read -r nodegroup; do
  [[ -n "${nodegroup}" ]] || continue
  log "Deleting nodegroup ${nodegroup}"
  aws eks delete-nodegroup --cluster-name "${AWS_EKS_NAME}" \
    --nodegroup-name "${nodegroup}" >/dev/null
done < <(aws eks list-nodegroups --cluster-name "${AWS_EKS_NAME}" \
  --query 'nodegroups[]' --output text | tr '\t' '\n')

deadline=$((SECONDS + timeout_seconds))
while (( SECONDS < deadline )); do
  remaining="$(aws eks list-nodegroups --cluster-name "${AWS_EKS_NAME}" \
    --query 'length(nodegroups)' --output text)"
  [[ "${remaining}" == "0" ]] && break
  log "Waiting for ${remaining} nodegroup(s) to finish deleting"
  sleep 20
done

log "Deleting cluster ${AWS_EKS_NAME}"
# No wait afterwards: the control plane deletion is asynchronous, like `az group delete --no-wait`
# and `gcloud container clusters delete --async` in the other two teardowns.
if ! aws eks delete-cluster --name "${AWS_EKS_NAME}" >/dev/null; then
  die "delete-cluster failed for ${AWS_EKS_NAME}, most likely a nodegroup still deleting. Re-run the cloud-cleanup workflow — the control plane bills by the hour until it is gone"
fi

log "Deletion of ${AWS_EKS_NAME} started"
