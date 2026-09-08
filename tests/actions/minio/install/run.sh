#!/usr/bin/env bash
# minio/install — deploy a single-node MinIO (S3-compatible object store) + ClusterIP Service into
# the Neo4j namespace and create the static-key credentials Secret both the backup Job and the Neo4j
# server pods use to reach it (ADR-016 object-store path, MinIO-testable). The bucket is created by a
# busybox initContainer that mkdir's it under MinIO's data dir (a top-level dir = a bucket in
# single-node mode), so no mc/client is needed. Idempotent: re-apply is a no-op. MinIO runs in the
# same namespace so its ClusterIP DNS (minio.<ns>.svc:9000) resolves for the backup Job and servers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../../lib/common.sh
source "${SCRIPT_DIR}/../../../lib/common.sh"

MINIO_IMAGE="${MINIO_IMAGE:-minio/minio:latest}"
INIT_IMAGE="${MINIO_INIT_IMAGE:-busybox:1.36}"
BUCKET="${S3_BUCKET:-neo4jbackups}"
SECRET="${S3_CREDS_SECRET:-e2e-s3-creds}"
ENDPOINT="http://minio.${NEO4J_NAMESPACE}.svc:9000"
ACCESS_KEY="${S3_ACCESS_KEY:-minioadmin}"
SECRET_KEY="${S3_SECRET_KEY:-minioadmin}"
REGION="${S3_REGION:-us-east-1}"

log "Deploying MinIO (bucket ${BUCKET}) into ${NEO4J_NAMESPACE}"
kubectl apply -n "${NEO4J_NAMESPACE}" -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio
  labels:
    app.kubernetes.io/name: minio
    app.kubernetes.io/managed-by: neo4j-e2e
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: minio
  template:
    metadata:
      labels:
        app.kubernetes.io/name: minio
    spec:
      initContainers:
        - name: make-bucket
          image: ${INIT_IMAGE}
          command: ["sh", "-c", "mkdir -p /data/${BUCKET}"]
          volumeMounts:
            - { name: data, mountPath: /data }
      containers:
        - name: minio
          image: ${MINIO_IMAGE}
          args: ["server", "/data", "--console-address", ":9001"]
          env:
            - { name: MINIO_ROOT_USER, value: "${ACCESS_KEY}" }
            - { name: MINIO_ROOT_PASSWORD, value: "${SECRET_KEY}" }
          ports:
            - { containerPort: 9000, name: s3 }
          readinessProbe:
            httpGet: { path: /minio/health/ready, port: 9000 }
            initialDelaySeconds: 5
            periodSeconds: 5
          volumeMounts:
            - { name: data, mountPath: /data }
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: minio
  labels:
    app.kubernetes.io/name: minio
    app.kubernetes.io/managed-by: neo4j-e2e
spec:
  selector:
    app.kubernetes.io/name: minio
  ports:
    - { name: s3, port: 9000, targetPort: 9000 }
EOF

log "Waiting for MinIO rollout (timeout 180s)"
if ! kubectl -n "${NEO4J_NAMESPACE}" rollout status deploy/minio --timeout=180s; then
  kubectl -n "${NEO4J_NAMESPACE}" describe deploy/minio >&2 || true
  kubectl -n "${NEO4J_NAMESPACE}" get pods -l app.kubernetes.io/name=minio -o wide >&2 || true
  die "MinIO deployment did not become available"
fi

# Static-key credentials Secret, projected verbatim as env into the backup Job (destination.
# credentials) and the server pods (cloudIdentity.staticKeySecret). AWS_ENDPOINT_URL_S3 points the
# AWS SDK v2 at MinIO; AWS_S3_DISABLE_MULTIPART_CHECKSUMS is the documented knob for custom S3
# endpoints. Neo4j uses these both for neo4j-admin backup (write) and CloudSeedProvider (read).
log "Creating S3 credentials Secret ${SECRET} (endpoint ${ENDPOINT})"
kubectl create secret generic "${SECRET}" -n "${NEO4J_NAMESPACE}" \
  --from-literal=AWS_ACCESS_KEY_ID="${ACCESS_KEY}" \
  --from-literal=AWS_SECRET_ACCESS_KEY="${SECRET_KEY}" \
  --from-literal=AWS_ENDPOINT_URL_S3="${ENDPOINT}" \
  --from-literal=AWS_ENDPOINT_URL="${ENDPOINT}" \
  --from-literal=AWS_REGION="${REGION}" \
  --from-literal=AWS_DEFAULT_REGION="${REGION}" \
  --from-literal=AWS_S3_DISABLE_MULTIPART_CHECKSUMS="true" \
  --dry-run=client -o yaml | kubectl apply -n "${NEO4J_NAMESPACE}" -f -

log "MinIO ready in ${NEO4J_NAMESPACE}; bucket ${BUCKET}, creds Secret ${SECRET}, endpoint ${ENDPOINT}"
