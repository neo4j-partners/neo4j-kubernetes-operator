#!/usr/bin/env bash
# minio/cleanup — remove the MinIO Deployment, Service, and the S3 credentials Secret so a re-run
# starts clean. Best-effort (teardown swallows failures). The emptyDir bucket dies with the pod.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

SECRET="${S3_CREDS_SECRET:-e2e-s3-creds}"

kubectl delete deploy minio -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=120s || true
kubectl delete service minio -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=60s || true
kubectl delete secret "${SECRET}" -n "${NEO4J_NAMESPACE}" --ignore-not-found --timeout=60s || true

log "MinIO cleanup done"
