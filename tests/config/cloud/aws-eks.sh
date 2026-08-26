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
# Comma-separated subnets spanning two availability zones. Empty means "discover the default VPC",
# which is what CI relies on; set it for an account without a default VPC, or to pin the network.
AWS_EKS_SUBNET_IDS="${AWS_EKS_SUBNET_IDS:-}"
AWS_EKS_NODEGROUP_NAME="${AWS_EKS_NODEGROUP_NAME:-default}"
# Bound on the nodegroup deletion wait in tests/aws/teardown-eks.sh. Draining and terminating the
# instances takes a few minutes; the aws waiter's own limit is 40, long enough to hold a runner for
# a deletion that is never going to finish.
AWS_EKS_TEARDOWN_TIMEOUT="${AWS_EKS_TEARDOWN_TIMEOUT:-900}"
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
