#!/usr/bin/env bash
# Build and push the operator image to Artifact Registry (after ensure-gke.sh).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"

require_cmd gcloud docker make

: "${OPERATOR_IMAGE:?OPERATOR_IMAGE required}"

# Registry host is the first path segment of the reference, so a caller that only carries
# OPERATOR_IMAGE through GITHUB_ENV needs nothing else.
AR_HOST="${AR_HOST:-${OPERATOR_IMAGE%%/*}}"

# Registers gcloud as a Docker credential helper for that host; without it the push is anonymous
# and Artifact Registry answers 403.
gcloud auth configure-docker "${AR_HOST}" --quiet

cd "${REPO_ROOT}"
# GKE nodes are amd64 whatever the machine driving the run.
DOCKER_PLATFORM=linux/amd64 make docker-build IMG="${OPERATOR_IMAGE}"
docker push "${OPERATOR_IMAGE}"

log "Pushed ${OPERATOR_IMAGE}"
