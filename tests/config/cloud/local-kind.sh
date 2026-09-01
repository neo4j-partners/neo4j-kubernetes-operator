#!/usr/bin/env bash
# Cloud profile: local kind cluster.

CLOUD_ID=local-kind
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-standard}"
OPERATOR_IMAGE="${OPERATOR_IMAGE:-neo4j-operator:local}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-neo4j-operator-ci}"

# The node image is what fixes the Kubernetes version under test. Left to itself kind picks its
# own default, which moves whenever the kind binary is updated — so the cluster would run whatever
# the installed kind happens to prefer instead of the version the operator claims to support.
KUBERNETES_VERSION="${KUBERNETES_VERSION:-${KUBERNETES_VERSION_PINNED}}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v${KUBERNETES_VERSION}}"
