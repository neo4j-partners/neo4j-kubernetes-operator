#!/usr/bin/env bash
# Build and push the operator image to ECR (after ensure-eks.sh).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../lib/common.sh
source "${REPO_ROOT}/tests/lib/common.sh"

require_cmd aws docker make

: "${OPERATOR_IMAGE:?OPERATOR_IMAGE required}"

# Registry host is the first path segment of the reference, so a caller that only carries
# OPERATOR_IMAGE through GITHUB_ENV needs nothing else.
ECR_HOST="${ECR_HOST:-${OPERATOR_IMAGE%%/*}}"
# Region is embedded in the host — <account>.dkr.ecr.<region>.amazonaws.com — and get-login-password
# must target the same one, otherwise the token is valid for a registry we are not pushing to.
AWS_REGION="${AWS_REGION:-${AWS_DEFAULT_REGION:-$(printf '%s' "${ECR_HOST}" | cut -d. -f4)}}"

# ECR tokens last 12 hours and are per-registry; without this the push is anonymous and ECR
# answers with a 401 that mentions no credentials at all.
aws ecr get-login-password --region "${AWS_REGION}" \
  | docker login --username AWS --password-stdin "${ECR_HOST}"

cd "${REPO_ROOT}"
# EKS managed nodegroups run amd64 whatever the machine driving the run.
DOCKER_PLATFORM=linux/amd64 make docker-build IMG="${OPERATOR_IMAGE}"
docker push "${OPERATOR_IMAGE}"

log "Pushed ${OPERATOR_IMAGE}"
