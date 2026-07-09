#!/usr/bin/env bash
# Cloud profile: local kind cluster.

CLOUD_ID=local-kind
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-standard}"
OPERATOR_IMAGE="${OPERATOR_IMAGE:-neo4j-operator:local}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-neo4j-operator-ci}"
