#!/usr/bin/env bash
# Prepare a local kind cluster for e2e (CI and laptop).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"
# shellcheck source=../config/reconcile.sh
source "${REPO_ROOT}/tests/config/reconcile.sh"
load_cloud_config local-kind

require_cmd kind docker kubectl

# What Kubernetes version a live cluster actually runs, read back from the node container rather
# than from kubectl: the point of the check below is to compare against the image that was asked
# for, and only the container records which one it was built from.
kind_running_node_image() {
  local node
  node="$(kind get nodes --name "${KIND_CLUSTER_NAME}" 2>/dev/null | head -n1)"
  [[ -n "${node}" ]] || return 1
  docker inspect --format '{{.Config.Image}}' "${node}" 2>/dev/null
}

if ! kind get clusters 2>/dev/null | grep -qx "${KIND_CLUSTER_NAME}"; then
  log "Creating kind cluster ${KIND_CLUSTER_NAME} on ${KIND_NODE_IMAGE}"
  kind create cluster --name "${KIND_CLUSTER_NAME}" --image "${KIND_NODE_IMAGE}"
else
  # Reusing a cluster built on another Kubernetes version would quietly test something other than
  # what was asked for, and the run would still report the requested version. Stop rather than
  # recreate: on a laptop that cluster may hold work worth keeping, and CI never hits this branch
  # because each job starts on a runner with no cluster at all.
  running="$(kind_running_node_image || true)"
  if [[ -n "${running}" && "${running}" != "${KIND_NODE_IMAGE}" ]]; then
    die "kind cluster ${KIND_CLUSTER_NAME} runs ${running} but ${KIND_NODE_IMAGE} was requested — delete it first: kind delete cluster --name ${KIND_CLUSTER_NAME}"
  fi
  log "Reusing kind cluster ${KIND_CLUSTER_NAME} on ${running:-an unrecognised node image}"
fi

kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"

cd "${REPO_ROOT}"

# E2E_REUSE_LOCAL_IMAGES=true: the caller has already put the images in the local Docker
# daemon (CI builds the operator once and shares it across the suite matrix, and restores
# the Neo4j image from cache). Building and pulling per job would mean one operator build
# and one Docker Hub pull for every suite — the pull in particular is rate-limited on CI
# runners, which is exactly what the pre-load below exists to avoid.
# Default (unset) keeps the laptop behaviour: always build, always pull.
REUSE_IMAGES="${E2E_REUSE_LOCAL_IMAGES:-false}"

if [[ "${REUSE_IMAGES}" == "true" ]] && docker image inspect "${OPERATOR_IMAGE}" >/dev/null 2>&1; then
  # Print when it was built. A stale operator image against a current manifest fails as
  # "flag provided but not defined: -<something>" — the binary predates a flag config/manager
  # now passes — and nothing in that message hints the image is the problem. In CI the
  # images job builds from the same commit in the same run, so this only bites locally.
  log "Reusing operator image ${OPERATOR_IMAGE} built $(docker image inspect "${OPERATOR_IMAGE}" --format '{{.Created}}') — rebuild it if the operator flags have changed since"
else
  docker build -t "${OPERATOR_IMAGE}" .
fi
# A kind older than the node image fails here with "unknown containerd config version", a message
# that names neither kind nor the version that was selected. Selecting a newer KUBERNETES_VERSION
# on a laptop is exactly how one reaches it, so name both.
if ! kind load docker-image "${OPERATOR_IMAGE}" --name "${KIND_CLUSTER_NAME}"; then
  die "could not load ${OPERATOR_IMAGE} into ${KIND_CLUSTER_NAME} — kind $(kind version | awk '{print $2}') may be too old for ${KIND_NODE_IMAGE}; upgrade kind, or select a KUBERNETES_VERSION your kind release publishes a node image for"
fi

# Pre-pull the Neo4j image once and load it into the node, so the FIRST Neo4j pod does not
# pay a cold Docker Hub pull (often rate-limited on CI runners) that can exceed the Ready
# wait. The image tag is not "latest", so pods use imagePullPolicy=IfNotPresent and reuse
# the cached node image. Best-effort: on failure, pods fall back to pulling on demand.
# Both come from tests/config/neo4j/base.sh, which reconcile.sh sourced above — repeating the
# defaults here is how this version drifted from the one the fixtures deploy.
if [[ "${NEO4J_EDITION}" == "enterprise" ]]; then
  NEO4J_IMAGE="neo4j:${NEO4J_VERSION}-enterprise"
else
  NEO4J_IMAGE="neo4j:${NEO4J_VERSION}"
fi
log "Pre-loading Neo4j image ${NEO4J_IMAGE} into kind (avoids per-pod Docker Hub pulls)"
# Every step stays inside a condition so this cannot abort the run under `set -e`. Two things go
# wrong in practice. The pull is rate-limited on CI runners, which is why the caller may pre-seed the
# image (E2E_REUSE_LOCAL_IMAGES). The load is what breaks on Docker Desktop for Apple silicon, where
# containerd refuses the multi-arch manifest with "content digest ... not found" — a single-platform
# archive gets around it.
load_neo4j_image() {
  if [[ "${REUSE_IMAGES}" == "true" ]] && docker image inspect "${NEO4J_IMAGE}" >/dev/null 2>&1; then
    log "Reusing Neo4j image ${NEO4J_IMAGE} already in the local daemon (no Docker Hub pull)"
  elif ! docker pull "${NEO4J_IMAGE}"; then
    return 1
  fi

  if kind load docker-image "${NEO4J_IMAGE}" --name "${KIND_CLUSTER_NAME}"; then
    return 0
  fi

  local archive
  archive="$(mktemp -t neo4j-image-XXXXXX.tar)" || return 1
  if docker save --platform "linux/$(docker version --format '{{.Server.Arch}}')" \
    -o "${archive}" "${NEO4J_IMAGE}" &&
    kind load image-archive "${archive}" --name "${KIND_CLUSTER_NAME}"; then
    log "Loaded ${NEO4J_IMAGE} as a single-platform archive"
    rm -f "${archive}"
    return 0
  fi
  rm -f "${archive}"
  return 1
}

if ! load_neo4j_image; then
  log "WARN: could not pre-load ${NEO4J_IMAGE}; Neo4j pods will pull it on demand"
fi

log "kind cluster ready"
