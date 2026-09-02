#!/usr/bin/env bash
# Cloud profile: Google Kubernetes Engine (CI and manual runs).

CLOUD_ID=gcp-gke
# GKE's default CSI class — pd-balanced, and WaitForFirstConsumer like kind's standard and AKS's
# managed-csi, so the storage asserts see the same binding sequence on all three.
STORAGE_CLASS_NAME="${STORAGE_CLASS_NAME:-standard-rwo}"
# Set by tests/gcp/ensure-gke.sh before run-e2e when not provided.
OPERATOR_IMAGE="${OPERATOR_IMAGE:-}"

# Project hosting the cluster, the Artifact Registry repository and the CI service account.
GCP_PROJECT="${GCP_PROJECT:-kop12345}"
# Region for Artifact Registry, zone for the cluster: a zonal cluster has one control plane and
# nodes in one zone, which is cheaper and quicker to create than a regional one. Nothing in the
# suites depends on cross-zone behaviour.
GCP_REGION="${GCP_REGION:-europe-west1}"
GCP_ZONE="${GCP_ZONE:-europe-west1-b}"
GCP_AR_REPOSITORY="${GCP_AR_REPOSITORY:-neo4j-operator-ci}"

GKE_CLUSTER_NAME="${GKE_CLUSTER_NAME:-neo4j-operator-ci-gke}"
GKE_NODE_COUNT="${GKE_NODE_COUNT:-2}"
# 4 vCPU / 16 GiB per node, matching the AKS profile's Standard_D4s_v3 so a Cluster suite has the
# same room on both clouds.
GKE_MACHINE_TYPE="${GKE_MACHINE_TYPE:-e2-standard-4}"

# Version for the control plane, passed to `gcloud container clusters create`. A minor is enough:
# GKE resolves it to a patch and its own -gke build number, which no one should have to track here.
KUBERNETES_VERSION="${KUBERNETES_VERSION:-${KUBERNETES_VERSION_GKE}}"
