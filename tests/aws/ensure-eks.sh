#!/usr/bin/env bash
# Ensure the ECR repository and the EKS cluster exist; configure kubectl.
#
# Source it, do not execute it: callers need OPERATOR_IMAGE afterwards, same contract as
# tests/azure/ensure-aks.sh and tests/gcp/ensure-gke.sh.
#
# Creates no IAM object. The CI identity holds PowerUserAccess, which excludes IAM, so the cluster
# role and the node role are provisioned out of band (tests/contribute.md, AWS CI setup) and only
# passed here. The same limit rules out IRSA for the EBS CSI driver: its OIDC provider is derived
# from the cluster's issuer URL, so a cluster recreated nightly would need a new provider each time,
# and that is an IAM object. EKS Pod Identity replaces it — an addon plus an EKS-side association,
# no IAM call at run time.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config aws-eks

# No separate authenticator: aws-cli v2 provides `aws eks get-token`, which is what
# update-kubeconfig writes into the kubeconfig.
require_cmd aws kubectl

# Every call below resolves the region from the environment, including the kubeconfig entry
# update-kubeconfig writes, so export both spellings once.
export AWS_REGION
export AWS_DEFAULT_REGION="${AWS_REGION}"

if [[ -z "${AWS_ACCOUNT_ID}" ]]; then
  AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
fi
log "AWS account ${AWS_ACCOUNT_ID}, region ${AWS_REGION}"

# `aws eks wait` writes nothing at all while it polls, for up to 40 minutes on a nodegroup. In a CI
# log that is indistinguishable from a hung step, and it keeps a runner busy long after a creation
# has stopped making progress. These poll the same API and print one line a minute instead.
_cluster_status() {
  aws eks describe-cluster --name "${AWS_EKS_NAME}" \
    --query 'cluster.status' --output text 2>/dev/null || true
}

_nodegroup_status() {
  aws eks describe-nodegroup --cluster-name "${AWS_EKS_NAME}" \
    --nodegroup-name "${AWS_EKS_NODEGROUP_NAME}" \
    --query 'nodegroup.status' --output text 2>/dev/null || true
}

# _await <probe fn> <label> <wanted> <transient states> <timeout s> [hint] [diagnostics fn]
# An empty wanted status means "gone", which is how deletion is awaited. Transient takes a
# space-separated list, because an addon reports DEGRADED while its pods start and only settles on
# ACTIVE afterwards. Any state outside that list ends the run: EKS leaves a failed nodegroup in
# place, and waiting on CREATE_FAILED only delays the report. The diagnostics function, when given,
# runs before giving up — the last chance to capture why, since the resource is about to be deleted.
_await() {
  local probe="$1" label="$2" want="$3" transient="$4" timeout="$5" hint="${6:-}" diag="${7:-}"
  local deadline=$((SECONDS + timeout)) started=${SECONDS} status
  while :; do
    status="$("${probe}")"
    if [[ "${status}" == "${want}" ]] \
      || [[ -z "${want}" && ( -z "${status}" || "${status}" == "None" ) ]]; then
      log "${label} is ${want:-gone} ($((SECONDS - started))s)"
      return 0
    fi
    if [[ " ${transient} " != *" ${status} "* ]]; then
      [[ -z "${diag}" ]] || "${diag}"
      die "${label} is ${status:-gone}, expected ${want:-gone}. ${hint}"
    fi
    if (( SECONDS >= deadline )); then
      [[ -z "${diag}" ]] || "${diag}"
      die "${label} is still ${status} after ${timeout}s — stopping rather than holding the runner. ${hint}"
    fi
    log "${label} is ${status} ($((SECONDS - started))s elapsed)"
    sleep 30
  done
}

# A plain name is expanded in the CI account; a full ARN is honoured as given, which allows a role
# shared from another account without a second variable.
_eks_role_arn() {
  case "$1" in
    arn:*) printf '%s' "$1" ;;
    *) printf 'arn:aws:iam::%s:role/%s' "${AWS_ACCOUNT_ID}" "$1" ;;
  esac
}
cluster_role_arn="$(_eks_role_arn "${AWS_EKS_CLUSTER_ROLE}")"
node_role_arn="$(_eks_role_arn "${AWS_EKS_NODE_ROLE}")"

# PowerUserAccess denies iam:GetRole but does allow iam:ListRoles, which is just enough to tell the
# two failure modes apart: a role nobody created, and a role this identity may not pass. AWS answers
# both with the same AccessDeniedException on create-cluster and deliberately does not say which.
# Best-effort — an identity without ListRoles must not fail the run over a check.
if aws iam list-roles --max-items 1 >/dev/null 2>&1; then
  for role in "${AWS_EKS_CLUSTER_ROLE}" "${AWS_EKS_NODE_ROLE}"; do
    # A full ARN can name a role in another account, where ListRoles says nothing.
    case "${role}" in arn:*) continue ;; esac
    [[ -n "$(aws iam list-roles --query "Roles[?RoleName=='${role}'].RoleName" --output text)" ]] \
      || die "IAM role ${role} does not exist in account ${AWS_ACCOUNT_ID}. Both cluster roles are provisioned out of band — see tests/contribute.md (AWS CI setup)"
  done
  log "Cluster and node roles both exist"
else
  log "This identity cannot list IAM roles — skipping the role preflight"
fi

# The node role serves two purposes and needs a trust statement for each: ec2.amazonaws.com for the
# instances, pods.eks.amazonaws.com for the EBS CSI controller through Pod Identity. EKS only checks
# the second when the association is created, minutes into the run and long after the nodegroup has
# been paid for. Checking here costs one call — GetRole is granted for CreateNodegroup anyway.
case "${AWS_EKS_NODE_ROLE}" in
  arn:*) ;; # A role in another account: GetRole says nothing useful.
  *)
    # to_string covers both spellings, since Principal.Service is a string for one service and a
    # list for several.
    node_role_trust="$(aws iam get-role --role-name "${AWS_EKS_NODE_ROLE}" \
      --query "length(Role.AssumeRolePolicyDocument.Statement[?contains(to_string(Principal.Service), 'pods.eks.amazonaws.com')])" \
      --output text 2>/dev/null || true)"
    if [[ "${node_role_trust}" == "0" ]]; then
      die "IAM role ${AWS_EKS_NODE_ROLE} does not trust pods.eks.amazonaws.com, so the EBS CSI controller cannot obtain credentials and every storage suite would fail. Whoever holds IAM in the account runs: aws iam update-assume-role-policy --role-name ${AWS_EKS_NODE_ROLE} --policy-document '{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"ec2.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"},{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"pods.eks.amazonaws.com\"},\"Action\":[\"sts:AssumeRole\",\"sts:TagSession\"]}]}' — see tests/contribute.md (AWS CI setup)"
    elif [[ -n "${node_role_trust}" && "${node_role_trust}" != "None" ]]; then
      log "Node role trusts pods.eks.amazonaws.com"
    fi
    ;;
esac

# The repository outlives the cluster deliberately. It holds a handful of image layers, costs
# cents, and re-creating it per run would mean pushing every layer again with nothing saved.
if ! aws ecr describe-repositories --repository-names "${AWS_ECR_REPOSITORY}" >/dev/null 2>&1; then
  log "Creating ECR repository ${AWS_ECR_REPOSITORY}"
  aws ecr create-repository --repository-name "${AWS_ECR_REPOSITORY}" >/dev/null
else
  log "ECR repository ${AWS_ECR_REPOSITORY} exists"
fi

# One ECR repository holds one image name, so the repository *is* the image path — unlike ACR and
# Artifact Registry, where the operator image is a name inside the registry.
ECR_HOST="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com"

# The state matters, not just the existence. delete-cluster returns immediately and the control
# plane takes minutes to go, so a cluster torn down by an earlier run keeps answering
# describe-cluster as DELETING — and reusing it would fail every step after this one.
cluster_status="$(_cluster_status)"

if [[ "${cluster_status}" == "DELETING" ]]; then
  log "Cluster ${AWS_EKS_NAME} is still being deleted by an earlier run — waiting before re-creating"
  _await _cluster_status "cluster ${AWS_EKS_NAME}" "" DELETING "${AWS_EKS_TEARDOWN_TIMEOUT}" \
    "Run the cloud-cleanup workflow, or delete it by hand, then start again"
  cluster_status=""
fi

case "${cluster_status}" in
  ACTIVE | CREATING)
    log "EKS cluster ${AWS_EKS_NAME} exists (${cluster_status})"
    # A cluster left CREATING by a cancelled run is worth waiting for rather than failing on.
    [[ "${cluster_status}" == "ACTIVE" ]] \
      || _await _cluster_status "cluster ${AWS_EKS_NAME}" ACTIVE CREATING "${AWS_EKS_CLUSTER_TIMEOUT}" \
        "An earlier run left it half-created; delete it and start again"
    # Reuse the cluster's own subnets: a nodegroup added later must land in the network the cluster
    # was created with, which is not necessarily what default-VPC discovery would return today.
    subnet_ids="$(aws eks describe-cluster --name "${AWS_EKS_NAME}" \
      --query 'cluster.resourcesVpcConfig.subnetIds' --output text | tr '\t' ',')"
    ;;
  "" | None)
    subnet_ids="${AWS_EKS_SUBNET_IDS:-}"
    if [[ -z "${subnet_ids}" ]]; then
      # Borrow the default VPC rather than create one per run: EKS needs two availability zones,
      # and a managed nodegroup in public subnets needs public IPs to reach ECR — both already true
      # of a default VPC, and creating a VPC would leave more to tear down.
      vpc_id="$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true \
        --query 'Vpcs[0].VpcId' --output text)"
      [[ -n "${vpc_id}" && "${vpc_id}" != "None" ]] \
        || die "no default VPC in ${AWS_REGION}. Create one, or set AWS_EKS_SUBNET_IDS to a comma-separated list of subnets spanning two availability zones"
      # One subnet per AZ, three at most: more buys nothing here, and some regions expose an AZ
      # that EKS does not support, so a deterministic slice beats passing every subnet.
      subnet_ids="$(aws ec2 describe-subnets --filters "Name=vpc-id,Values=${vpc_id}" \
        --query 'Subnets[].[AvailabilityZone,SubnetId]' --output text \
        | sort | awk '!seen[$1]++ {print $2}' | head -3 | paste -sd, -)"
    fi
    [[ "${subnet_ids}" == *,* ]] \
      || die "need subnets in at least two availability zones, got '${subnet_ids}'. Set AWS_EKS_SUBNET_IDS explicitly"

    log "Creating EKS cluster ${AWS_EKS_NAME} in ${subnet_ids} (10-15 minutes)"
    # authenticationMode=API with bootstrapClusterCreatorAdminPermissions gives the creating
    # identity an access entry with cluster-admin, which is what makes update-kubeconfig below
    # usable. Anyone else — including the human who owns the account — needs their own entry.
    if ! aws eks create-cluster \
      --name "${AWS_EKS_NAME}" \
      --role-arn "${cluster_role_arn}" \
      --resources-vpc-config "subnetIds=${subnet_ids},endpointPublicAccess=true" \
      --access-config "authenticationMode=API,bootstrapClusterCreatorAdminPermissions=true" \
      >/dev/null; then
      die "create-cluster was refused. Under PowerUserAccess this is the identity's rights over ${cluster_role_arn}: iam:PassRole, plus the read actions EKS calls on the roles it touches. The preflight above says whether the role itself exists. See tests/contribute.md (AWS CI setup)"
    fi
    _await _cluster_status "cluster ${AWS_EKS_NAME}" ACTIVE CREATING "${AWS_EKS_CLUSTER_TIMEOUT}" \
      "Check the cluster in the EKS console before re-running"
    ;;
  *)
    # FAILED, or a state a future API version introduces. Guessing what to do with a cluster in an
    # unknown state is how a run destroys something it should not have touched.
    die "EKS cluster ${AWS_EKS_NAME} is in state ${cluster_status}. Delete it — make teardown-e2e-eks — and run again"
    ;;
esac

# --subnets takes a space-separated list where the VPC config took a comma-separated one.
IFS=',' read -r -a subnet_list <<<"${subnet_ids}"

# A nodegroup that fails to come up says why in health.issues — no capacity in the AZ, a subnet
# without a route to ECR, an instance type the region does not offer — and the status alone does not.
nodegroup_health_hint="Ask why with: aws eks describe-nodegroup --cluster-name ${AWS_EKS_NAME} --nodegroup-name ${AWS_EKS_NODEGROUP_NAME} --query nodegroup.health"

# Same story as the cluster: deletion is asynchronous, so a nodegroup on its way out still answers
# describe-nodegroup, and one left CREATING by a cancelled run is worth waiting for.
nodegroup_status="$(_nodegroup_status)"

if [[ "${nodegroup_status}" == "DELETING" ]]; then
  log "Nodegroup ${AWS_EKS_NODEGROUP_NAME} is still being deleted — waiting before re-creating"
  _await _nodegroup_status "nodegroup ${AWS_EKS_NODEGROUP_NAME}" "" DELETING \
    "${AWS_EKS_TEARDOWN_TIMEOUT}" "Delete it by hand, then start again"
  nodegroup_status=""
fi

case "${nodegroup_status}" in
  ACTIVE | UPDATING)
    log "Nodegroup ${AWS_EKS_NODEGROUP_NAME} exists (${nodegroup_status})"
    ;;
  CREATING)
    log "Nodegroup ${AWS_EKS_NODEGROUP_NAME} is being created by another run — waiting"
    _await _nodegroup_status "nodegroup ${AWS_EKS_NODEGROUP_NAME}" ACTIVE CREATING \
      "${AWS_EKS_NODEGROUP_TIMEOUT}" "${nodegroup_health_hint}"
    ;;
  "" | None)
    log "Creating nodegroup ${AWS_EKS_NODEGROUP_NAME} (${AWS_EKS_NODE_COUNT} x ${AWS_EKS_NODE_INSTANCE_TYPE})"
    # Fixed size, min = max = desired: nothing here benefits from autoscaling, and a stable node
    # count keeps the Cluster suite's placement assertions meaningful.
    if ! aws eks create-nodegroup \
      --cluster-name "${AWS_EKS_NAME}" \
      --nodegroup-name "${AWS_EKS_NODEGROUP_NAME}" \
      --node-role "${node_role_arn}" \
      --subnets "${subnet_list[@]}" \
      --instance-types "${AWS_EKS_NODE_INSTANCE_TYPE}" \
      --scaling-config "minSize=${AWS_EKS_NODE_COUNT},maxSize=${AWS_EKS_NODE_COUNT},desiredSize=${AWS_EKS_NODE_COUNT}" \
      >/dev/null; then
      die "create-nodegroup was refused. It passes ${node_role_arn} and also reads roles on the caller's behalf — including the AWSServiceRoleForAmazonEKSNodegroup service-linked role, which no resource list can name in advance. The read actions must therefore be granted on every role, not only the two above: see tests/contribute.md (AWS CI setup)"
    fi
    _await _nodegroup_status "nodegroup ${AWS_EKS_NODEGROUP_NAME}" ACTIVE CREATING \
      "${AWS_EKS_NODEGROUP_TIMEOUT}" "${nodegroup_health_hint}"
    ;;
  *)
    # CREATE_FAILED or DEGRADED: the instances are unusable and every suite would fail on
    # unschedulable pods. Say so here rather than 20 minutes later.
    die "Nodegroup ${AWS_EKS_NODEGROUP_NAME} is in state ${nodegroup_status}. Delete it — make teardown-e2e-eks — and run again. ${nodegroup_health_hint}"
    ;;
esac

# Before the addon, not after: an addon that never reaches ACTIVE can only be explained from inside
# the cluster, and a kubeconfig obtained afterwards arrives too late to say why.
aws eks update-kubeconfig --name "${AWS_EKS_NAME}"
log "kubectl context configured for ${AWS_EKS_NAME}"
kubectl get nodes -o wide

# A nodegroup reports ACTIVE once its instances register, which is not the same as Ready. The addon
# below needs somewhere to run, so a node short of Ready would surface minutes later as an addon
# timeout, blaming the addon for what is a node problem.
log "Waiting for every node to be Ready"
if ! kubectl wait --for=condition=Ready nodes --all \
  --timeout="${AWS_EKS_NODE_READY_TIMEOUT}s"; then
  kubectl describe nodes || true
  die "the nodes of ${AWS_EKS_NODEGROUP_NAME} registered but never became Ready. ${nodegroup_health_hint}"
fi

_addon_status() {
  aws eks describe-addon --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver \
    --query 'addon.status' --output text 2>/dev/null || true
}

_pod_identity_agent_status() {
  aws eks describe-addon --cluster-name "${AWS_EKS_NAME}" --addon-name eks-pod-identity-agent \
    --query 'addon.status' --output text 2>/dev/null || true
}

# EKS stops a pod from reaching the node's instance metadata, so the CSI controller cannot borrow the
# node role's credentials the way a process on the host would: it needs credentials of its own. Pod
# Identity delivers them without an IAM call — the agent is an addon, the association is an EKS API
# call, and the role it hands out is the node role, which already carries AmazonEBSCSIDriverPolicy
# and, provisioned out of band, trusts pods.eks.amazonaws.com as well (tests/contribute.md).
if [[ "$(_pod_identity_agent_status)" == "ACTIVE" ]]; then
  log "Addon eks-pod-identity-agent is ACTIVE"
else
  log "Installing the eks-pod-identity-agent addon"
  aws eks create-addon --cluster-name "${AWS_EKS_NAME}" \
    --addon-name eks-pod-identity-agent >/dev/null
  _await _pod_identity_agent_status "addon eks-pod-identity-agent" ACTIVE "CREATING DEGRADED" \
    "${AWS_EKS_ADDON_TIMEOUT}" "It is a DaemonSet on nodes that are already Ready, so look at the pods"
fi

# The association outlives the addon and a second create is an error rather than a no-op, so ask
# first. Naming a service account that does not exist yet is fine: the addon creates it below, and
# the agent only resolves the association when a pod using it starts.
pod_identity_assoc="$(aws eks list-pod-identity-associations --cluster-name "${AWS_EKS_NAME}" \
  --namespace kube-system --service-account ebs-csi-controller-sa \
  --query 'associations[0].associationId' --output text 2>/dev/null || true)"
if [[ -z "${pod_identity_assoc}" || "${pod_identity_assoc}" == "None" ]]; then
  log "Associating kube-system/ebs-csi-controller-sa with ${node_role_arn}"
  if ! aws eks create-pod-identity-association --cluster-name "${AWS_EKS_NAME}" \
    --namespace kube-system --service-account ebs-csi-controller-sa \
    --role-arn "${node_role_arn}" >/dev/null; then
    die "create-pod-identity-association was refused. It needs iam:PassRole on ${node_role_arn}, and that role must trust pods.eks.amazonaws.com for sts:AssumeRole and sts:TagSession — see tests/contribute.md (AWS CI setup)"
  fi
else
  log "Pod identity association for ebs-csi-controller-sa exists"
fi

# An addon stuck short of ACTIVE says why in two places and neither is the status: health.issues on
# the AWS side, and the controller's own pods on the cluster side.
_addon_diagnostics() {
  log "Addon health issues:"
  aws eks describe-addon --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver \
    --query 'addon.health.issues' --output json || true
  log "Nodes:"
  kubectl get nodes -o wide || true
  log "kube-system pods:"
  kubectl get pods -n kube-system -o wide || true
  log "Recent kube-system events:"
  kubectl get events -n kube-system --sort-by=.lastTimestamp 2>/dev/null | tail -30 || true
  # The sidecars only ever complain that the driver's socket is absent; ebs-plugin is the one that
  # says why — a credentials or metadata failure names itself here and nowhere else.
  log "ebs-plugin container, current and previous:"
  kubectl logs -n kube-system -l app=ebs-csi-controller -c ebs-plugin \
    --tail=40 --prefix 2>/dev/null || true
  kubectl logs -n kube-system -l app=ebs-csi-controller -c ebs-plugin \
    --tail=20 --previous --prefix 2>/dev/null || true
}

# EKS installs no CSI driver, so without this addon every PVC stays Pending and each storage suite
# fails on a pod that never schedules. Installed last: its controller needs nodes to run on, and
# credentials to serve, both of which exist by now.
addon_status="$(_addon_status)"

# A DEGRADED addon is reinstalled rather than reported. The driver crash-loops when it starts without
# credentials and does not recover once they arrive, so a cluster kept from an earlier run would stay
# broken for as long as it lives.
if [[ "${addon_status}" == "DEGRADED" ]]; then
  log "Addon aws-ebs-csi-driver is DEGRADED — removing it to install it again"
  _addon_diagnostics
  aws eks delete-addon --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver >/dev/null
  _await _addon_status "addon aws-ebs-csi-driver" "" DELETING "${AWS_EKS_ADDON_TIMEOUT}" \
    "Delete it by hand, then run again"
  addon_status=""
fi

case "${addon_status}" in
  ACTIVE)
    log "Addon aws-ebs-csi-driver is ACTIVE"
    ;;
  "" | None)
    log "Installing the aws-ebs-csi-driver addon"
    aws eks create-addon --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver >/dev/null
    _await _addon_status "addon aws-ebs-csi-driver" ACTIVE "CREATING DEGRADED" "${AWS_EKS_ADDON_TIMEOUT}" \
      "The controller gets its credentials from the pod identity association above; a crash loop in ebs-plugin means it did not" \
      _addon_diagnostics
    ;;
  *)
    _addon_diagnostics
    die "Addon aws-ebs-csi-driver is ${addon_status}, not ACTIVE. Delete the cluster — make teardown-e2e-eks — and run again"
    ;;
esac

# Cluster setup, not fixture data: AKS and GKE ship the class the storage suites ask for, EKS ships
# only gp2 and serves it through in-tree CSI migration. Declaring the provisioner explicitly removes
# that indirection, and WaitForFirstConsumer matches the binding sequence the storage asserts already
# expect on the other three platforms.
#
# Applied unconditionally rather than only when absent: a cluster kept from an earlier run may carry
# this class in an older shape, and everything here except the annotation is immutable anyway, so
# apply either changes nothing or adds what is missing.
log "Applying StorageClass ${STORAGE_CLASS_NAME}"
kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${STORAGE_CLASS_NAME}
  annotations:
    # Recent EKS versions ship no default class, while kind, AKS and GKE all do. Most fixtures leave
    # storageClassName unset and bind to the default, so without this every PVC stays Pending and no
    # suite gets past its first case.
    storageclass.kubernetes.io/is-default-class: "true"
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
EOF

# Two classes claiming the default make Kubernetes honour neither, which looks exactly like the
# failure the annotation above is here to prevent. Listed name by name rather than with a jsonpath
# filter: a filter that the client rejects returns nothing, and nothing reads as "all is well".
default_classes="$(kubectl get storageclass \
  -o jsonpath='{range .items[*]}{.metadata.name}{"="}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}' \
  | awk -F= '$2 == "true" { print $1 }')"
default_count="$(printf '%s' "${default_classes}" | awk 'NF' | wc -l | tr -d ' ')"
log "Default StorageClass: $(printf '%s' "${default_classes}" | tr '\n' ' ' | sed 's/ $//')${default_classes:+ }(${default_count})"
case "${default_count}" in
  1) ;;
  0) die "no default StorageClass after applying ${STORAGE_CLASS_NAME}, so every fixture that omits storageClassName would stay Pending" ;;
  *) die "${default_count} default StorageClasses, so Kubernetes will honour none of them. Remove the annotation from all but ${STORAGE_CLASS_NAME}" ;;
esac

export AWS_ACCOUNT_ID ECR_HOST
export OPERATOR_IMAGE="${ECR_HOST}/${AWS_ECR_REPOSITORY}:ci-${GITHUB_SHA:-local}"
