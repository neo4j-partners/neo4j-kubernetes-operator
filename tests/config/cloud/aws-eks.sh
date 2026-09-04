#!/usr/bin/env bash
# Cloud profile: Amazon EKS (CI and manual runs).

CLOUD_ID=aws-eks
# EKS ships no gp3 class and its default gp2 class only works through in-tree CSI migration, so
# tests/aws/ensure-eks.sh creates this one against ebs.csi.aws.com with WaitForFirstConsumer —
# the same binding sequence as kind's standard, AKS's managed-csi and GKE's standard-rwo.
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-gp3}"
# Set by tests/aws/ensure-eks.sh before run-e2e when not provided.
OPERATOR_IMAGE="${OPERATOR_IMAGE:-}"

# Region for the cluster and the ECR registry. Resolved from the ambient AWS environment when
# neither this nor AWS_DEFAULT_REGION is set, so a laptop with a configured profile needs nothing.
AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-eu-west-1}}"
# Account hosting the cluster, the ECR repository and the CI user. Derived from the caller's
# identity when unset, which keeps the account id out of the repository.
AWS_ACCOUNT_ID="${AWS_ACCOUNT_ID:-}"
AWS_ECR_REPOSITORY="${AWS_ECR_REPOSITORY:-neo4j-operator-ci}"

AWS_EKS_NAME="${AWS_EKS_NAME:-neo4j-operator-ci-eks}"
# Version for the control plane, passed to `aws eks create-cluster`. EKS takes a minor and rejects
# a patch outright, so tests/aws/ensure-eks.sh truncates anything longer before it calls the API.
# Only 1.36, 1.35 and 1.34 are in standard support; older minors still work but bill an extra
# per-cluster-hour extended-support fee, which is not something a test run should opt into quietly.
KUBERNETES_VERSION="${KUBERNETES_VERSION:-${KUBERNETES_VERSION_EKS}}"
# Comma-separated subnets spanning two availability zones. Empty means "discover the default VPC",
# which is what CI relies on; set it for an account without a default VPC, or to pin the network.
AWS_EKS_SUBNET_IDS="${AWS_EKS_SUBNET_IDS:-}"
AWS_EKS_NODEGROUP_NAME="${AWS_EKS_NODEGROUP_NAME:-default}"
# Bound on the nodegroup deletion wait in tests/aws/teardown-eks.sh. Draining and terminating the
# instances takes a few minutes; the aws waiter's own limit is 40, long enough to hold a runner for
# a deletion that is never going to finish.
AWS_EKS_TEARDOWN_TIMEOUT="${AWS_EKS_TEARDOWN_TIMEOUT:-900}"
# Bounds on the two creations, both polled by tests/aws/ensure-eks.sh rather than left to
# `aws eks wait`, which prints nothing and allows 20 minutes on a cluster and 40 on a nodegroup.
# A control plane takes 10 to 15 minutes, a nodegroup 3 to 6; past these values the creation is
# not slow, it is stuck, and holding the runner buys nothing.
AWS_EKS_CLUSTER_TIMEOUT="${AWS_EKS_CLUSTER_TIMEOUT:-1500}"
AWS_EKS_NODEGROUP_TIMEOUT="${AWS_EKS_NODEGROUP_TIMEOUT:-900}"
# The EBS CSI addon is a Deployment and a DaemonSet on nodes that already exist, so it is a matter
# of pulling images: a minute or two when it works, and never when the nodes are the problem.
AWS_EKS_ADDON_TIMEOUT="${AWS_EKS_ADDON_TIMEOUT:-420}"
# A registered instance is not a Ready node: the CNI has to start first. Checked separately so an
# unready node is not reported as an addon failure.
AWS_EKS_NODE_READY_TIMEOUT="${AWS_EKS_NODE_READY_TIMEOUT:-300}"
AWS_EKS_NODE_COUNT="${AWS_EKS_NODE_COUNT:-2}"
# 4 vCPU / 16 GiB per node, matching AKS's Standard_D4s_v3 and GKE's e2-standard-4 so a Cluster
# suite has the same room on all three clouds.
AWS_EKS_NODE_INSTANCE_TYPE="${AWS_EKS_NODE_INSTANCE_TYPE:-m5.xlarge}"

# EKS demands two pre-existing IAM roles, and the CI identity holds PowerUserAccess, which
# excludes IAM: it can pass these roles but never create them. Both are provisioned out of band —
# names, trust policies and attached policies in tests/contribute.md. A plain name is expanded to
# an ARN in the CI account; a full `arn:...` value is used as given, for a role in another account.
AWS_EKS_CLUSTER_ROLE="${AWS_EKS_CLUSTER_ROLE:-neo4j-operator-ci-eks-cluster-role}"
AWS_EKS_NODE_ROLE="${AWS_EKS_NODE_ROLE:-neo4j-operator-ci-eks-node-role}"
