#!/usr/bin/env bash
# Ensure the Artifact Registry repository and the GKE cluster exist; configure kubectl.
#
# Source it, do not execute it: callers need OPERATOR_IMAGE afterwards, same contract as
# tests/azure/ensure-aks.sh.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config gcp-gke

# kubectl talks to GKE through gke-gcloud-auth-plugin, a separate gcloud component. Without it
# get-credentials writes a kubeconfig that every later kubectl call rejects, several steps away
# from the cause.
require_cmd gcloud kubectl gke-gcloud-auth-plugin

: "${GCP_PROJECT:?GCP_PROJECT required}"

log "GCP project: ${GCP_PROJECT}"
gcloud config set project "${GCP_PROJECT}" >/dev/null

# The repository outlives the cluster deliberately. It holds a handful of image layers, costs
# cents, and re-creating it per run would mean pushing every layer again with nothing saved.
if ! gcloud artifacts repositories describe "${GCP_AR_REPOSITORY}" \
  --location "${GCP_REGION}" >/dev/null 2>&1; then
  log "Creating Artifact Registry repository ${GCP_AR_REPOSITORY} in ${GCP_REGION}"
  gcloud artifacts repositories create "${GCP_AR_REPOSITORY}" \
    --repository-format=docker \
    --location "${GCP_REGION}" \
    --description="Neo4j operator e2e images" >/dev/null
else
  log "Artifact Registry repository ${GCP_AR_REPOSITORY} exists"
fi

AR_HOST="${GCP_REGION}-docker.pkg.dev"

if ! gcloud container clusters describe "${GKE_CLUSTER_NAME}" \
  --zone "${GCP_ZONE}" >/dev/null 2>&1; then
  log "Creating GKE cluster ${GKE_CLUSTER_NAME} in ${GCP_ZONE} on Kubernetes ${KUBERNETES_VERSION}"
  gcloud container clusters create "${GKE_CLUSTER_NAME}" \
    --zone "${GCP_ZONE}" \
    --cluster-version "${KUBERNETES_VERSION}" \
    --num-nodes "${GKE_NODE_COUNT}" \
    --machine-type "${GKE_MACHINE_TYPE}" \
    --quiet
else
  gke_running="$(gcloud container clusters describe "${GKE_CLUSTER_NAME}" \
    --zone "${GCP_ZONE}" --format="value(currentMasterVersion)")"
  require_cluster_version "GKE cluster ${GKE_CLUSTER_NAME}" "${gke_running}" "${KUBERNETES_VERSION}" \
    "delete it first: gcloud container clusters delete ${GKE_CLUSTER_NAME} --zone ${GCP_ZONE} --quiet"
  log "GKE cluster ${GKE_CLUSTER_NAME} exists on Kubernetes ${gke_running}"
fi

# Nodes pull the operator image straight from Artifact Registry, so the node identity needs read
# access to this repository. Bound on the repository rather than the project: creating the
# repository already implies the rights to grant it, where project-level IAM would need more.
#
# Best-effort on purpose. The binding is usually already there — the default compute service
# account is a project Editor unless someone removed it — and a CI service account without
# setIamPolicy must not fail the run over a grant that is probably redundant. A missing grant
# surfaces later as ImagePullBackOff on the operator Deployment.
node_sa="$(gcloud container clusters describe "${GKE_CLUSTER_NAME}" --zone "${GCP_ZONE}" \
  --format='value(nodeConfig.serviceAccount)' 2>/dev/null || true)"
if [[ -z "${node_sa}" || "${node_sa}" == "default" ]]; then
  project_number="$(gcloud projects describe "${GCP_PROJECT}" --format='value(projectNumber)' 2>/dev/null || true)"
  node_sa="${project_number:+${project_number}-compute@developer.gserviceaccount.com}"
fi
if [[ -n "${node_sa}" ]]; then
  if gcloud artifacts repositories add-iam-policy-binding "${GCP_AR_REPOSITORY}" \
    --location "${GCP_REGION}" \
    --member="serviceAccount:${node_sa}" \
    --role=roles/artifactregistry.reader \
    --quiet >/dev/null 2>&1; then
    log "Node service account ${node_sa} can read ${GCP_AR_REPOSITORY}"
  else
    log "Could not grant artifactregistry.reader to ${node_sa} — continuing, pulls fail later if it was missing"
  fi
fi

gcloud container clusters get-credentials "${GKE_CLUSTER_NAME}" --zone "${GCP_ZONE}"

export AR_HOST
export OPERATOR_IMAGE="${AR_HOST}/${GCP_PROJECT}/${GCP_AR_REPOSITORY}/neo4j-operator:ci-${GITHUB_SHA:-local}"

log "kubectl context configured for ${GKE_CLUSTER_NAME}"
kubectl get nodes
