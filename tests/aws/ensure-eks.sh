#!/usr/bin/env bash
# Ensure the ECR repository and the EKS cluster exist; configure kubectl.
#
# Source it, do not execute it: callers need OPERATOR_IMAGE afterwards, same contract as
# tests/azure/ensure-aks.sh and tests/gcp/ensure-gke.sh.
#
# Creates no IAM object. The CI identity holds PowerUserAccess, which excludes IAM, so the cluster
# role and the node role are provisioned out of band (tests/contribute.md, AWS CI setup) and only
# passed here. The same limit is why the EBS CSI driver runs on the node role rather than through
# IRSA, which would need an OIDC provider this identity cannot create.

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

if aws eks describe-cluster --name "${AWS_EKS_NAME}" >/dev/null 2>&1; then
  log "EKS cluster ${AWS_EKS_NAME} exists"
  # Reuse the cluster's own subnets: a nodegroup added later must land in the network the cluster
  # was created with, which is not necessarily what default-VPC discovery would return today.
  subnet_ids="$(aws eks describe-cluster --name "${AWS_EKS_NAME}" \
    --query 'cluster.resourcesVpcConfig.subnetIds' --output text | tr '\t' ',')"
else
  subnet_ids="${AWS_EKS_SUBNET_IDS:-}"
  if [[ -z "${subnet_ids}" ]]; then
    # Borrow the default VPC rather than create one per run: EKS needs two availability zones, and
    # a managed nodegroup in public subnets needs public IPs to reach ECR — both already true of a
    # default VPC, and creating a VPC would leave more to tear down.
    vpc_id="$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true \
      --query 'Vpcs[0].VpcId' --output text)"
    [[ -n "${vpc_id}" && "${vpc_id}" != "None" ]] \
      || die "no default VPC in ${AWS_REGION}. Create one, or set AWS_EKS_SUBNET_IDS to a comma-separated list of subnets spanning two availability zones"
    # One subnet per AZ, three at most: more buys nothing here, and some regions expose an AZ that
    # EKS does not support, so a deterministic slice beats passing every subnet.
    subnet_ids="$(aws ec2 describe-subnets --filters "Name=vpc-id,Values=${vpc_id}" \
      --query 'Subnets[].[AvailabilityZone,SubnetId]' --output text \
      | sort | awk '!seen[$1]++ {print $2}' | head -3 | paste -sd, -)"
  fi
  [[ "${subnet_ids}" == *,* ]] \
    || die "need subnets in at least two availability zones, got '${subnet_ids}'. Set AWS_EKS_SUBNET_IDS explicitly"

  log "Creating EKS cluster ${AWS_EKS_NAME} in ${subnet_ids} (10-15 minutes)"
  # authenticationMode=API with bootstrapClusterCreatorAdminPermissions gives the creating identity
  # an access entry with cluster-admin, which is what makes update-kubeconfig below usable. Anyone
  # else — including the human who owns the account — needs an access entry of their own.
  if ! aws eks create-cluster \
    --name "${AWS_EKS_NAME}" \
    --role-arn "${cluster_role_arn}" \
    --resources-vpc-config "subnetIds=${subnet_ids},endpointPublicAccess=true" \
    --access-config "authenticationMode=API,bootstrapClusterCreatorAdminPermissions=true" \
    >/dev/null; then
    die "create-cluster was refused. Under PowerUserAccess this is iam:PassRole on ${cluster_role_arn} — the preflight above says whether the role itself exists. See tests/contribute.md (AWS CI setup)"
  fi
  aws eks wait cluster-active --name "${AWS_EKS_NAME}"
fi

# --subnets takes a space-separated list where the VPC config took a comma-separated one.
IFS=',' read -r -a subnet_list <<<"${subnet_ids}"

if ! aws eks describe-nodegroup --cluster-name "${AWS_EKS_NAME}" \
  --nodegroup-name "${AWS_EKS_NODEGROUP_NAME}" >/dev/null 2>&1; then
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
    die "create-nodegroup was refused. Under PowerUserAccess this is iam:PassRole on ${node_role_arn} — the preflight above says whether the role itself exists. See tests/contribute.md (AWS CI setup)"
  fi
  aws eks wait nodegroup-active --cluster-name "${AWS_EKS_NAME}" \
    --nodegroup-name "${AWS_EKS_NODEGROUP_NAME}"
else
  log "Nodegroup ${AWS_EKS_NODEGROUP_NAME} exists"
fi

# EKS installs no CSI driver, so without this addon every PVC stays Pending and each storage
# suite fails on a pod that never schedules. Installed after the nodegroup because its controller
# needs somewhere to run. No --service-account-role-arn: the controller then uses the node role's
# credentials, which is why that role carries AmazonEBSCSIDriverPolicy.
if ! aws eks describe-addon --cluster-name "${AWS_EKS_NAME}" \
  --addon-name aws-ebs-csi-driver >/dev/null 2>&1; then
  log "Installing the aws-ebs-csi-driver addon"
  aws eks create-addon --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver >/dev/null
  aws eks wait addon-active --cluster-name "${AWS_EKS_NAME}" --addon-name aws-ebs-csi-driver
else
  log "Addon aws-ebs-csi-driver present"
fi

aws eks update-kubeconfig --name "${AWS_EKS_NAME}"

# Cluster setup, not fixture data: AKS and GKE ship the class the storage suites ask for, EKS
# ships only gp2 and serves it through in-tree CSI migration. Declaring the provisioner explicitly
# removes that indirection, and WaitForFirstConsumer matches the binding sequence the storage
# asserts already expect on the other three platforms.
if ! kubectl get storageclass "${STORAGE_CLASS_NAME}" >/dev/null 2>&1; then
  log "Creating StorageClass ${STORAGE_CLASS_NAME}"
  kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${STORAGE_CLASS_NAME}
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
EOF
fi

export AWS_ACCOUNT_ID ECR_HOST
export OPERATOR_IMAGE="${ECR_HOST}/${AWS_ECR_REPOSITORY}:ci-${GITHUB_SHA:-local}"

log "kubectl context configured for ${AWS_EKS_NAME}"
kubectl get nodes
