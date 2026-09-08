#!/usr/bin/env bash
# minio/install verify — MinIO is available and the credentials Secret exists, so the backup Job and
# server pods can resolve and authenticate to the object store.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

SECRET="${S3_CREDS_SECRET:-e2e-s3-creds}"

kubectl -n "${NEO4J_NAMESPACE}" get deploy/minio >/dev/null 2>&1 \
  || die "MinIO deployment missing — did minio/install run?"
avail="$(kubectl -n "${NEO4J_NAMESPACE}" get deploy/minio -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true)"
[[ "${avail:-0}" -ge 1 ]] || die "MinIO not available (availableReplicas=${avail:-0})"
kubectl -n "${NEO4J_NAMESPACE}" get secret "${SECRET}" >/dev/null 2>&1 \
  || die "S3 credentials Secret ${SECRET} missing"

log "MinIO verify OK (deployment available, Secret ${SECRET} present)"
