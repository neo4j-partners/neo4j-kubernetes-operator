#!/usr/bin/env bash
#
# helm-sync-rbac.sh
#
# Regenerates charts/neo4j-operator/templates/clusterrole.yaml from
# config/rbac/role.yaml so the Helm install grants exactly the same
# permissions as the controller's kubebuilder +kubebuilder:rbac markers
# request — no more drift between the kustomize and Helm distributions.
#
# Source of truth: +kubebuilder:rbac:* markers on controller types.
# Pipeline:
#   make manifests        →  controller-gen writes config/rbac/role.yaml
#   make helm-sync-rbac   →  this script writes templates/clusterrole.yaml
#
# Edit the markers, not the rendered files.

set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC="${ROOT}/config/rbac/role.yaml"
DST="${ROOT}/charts/neo4j-operator/templates/clusterrole.yaml"
YQ="${YQ:-${ROOT}/bin/yq}"

if [[ ! -x "$YQ" ]]; then
    echo "error: yq not found at $YQ. Run 'make yq' first." >&2
    exit 1
fi

if [[ ! -f "$SRC" ]]; then
    echo "error: $SRC not found. Run 'make manifests' first." >&2
    exit 1
fi

# Sanity-check: the source file must be the kubebuilder-generated manager role.
NAME=$("$YQ" '.metadata.name' "$SRC")
if [[ "$NAME" != "manager-role" ]]; then
    echo "error: $SRC does not look like the kubebuilder-generated manager role" >&2
    echo "       (.metadata.name = $NAME, expected 'manager-role')" >&2
    exit 1
fi

# Partition the rules into two groups:
#   1. "core" — rules the operator always needs.
#   2. "external-secrets" — rules for the optional external-secrets.io
#      integration. Only emitted when .Values.rbac.externalSecretsIntegration
#      is true on the helm side, since the integration is opt-in (the
#      operator only creates ExternalSecret resources when a Neo4j CR sets
#      spec.{tls,auth}.externalSecrets.enabled=true). Without this split,
#      the chart's ClusterRole granted external-secrets CRUD verbs
#      cluster-wide on every install — wide blast radius for a feature
#      most users don't use. Per the November 2025 security review #1.
#
# yq writes each at zero indent which is the correct YAML form under a
# `rules:` parent key.
CORE_RULES=$("$YQ" '[.rules[] | select(.apiGroups | contains(["external-secrets.io"]) | not)]' "$SRC")
EXT_SECRETS_RULES=$("$YQ" '[.rules[] | select(.apiGroups | contains(["external-secrets.io"]))]' "$SRC")

# Helm template wrapper: gated by .Values.rbac.create AND the chart's helper
# that decides whether to emit ClusterRole vs Role (cluster-wide vs single-
# namespace operatorMode).  We preserve the existing chart conventions —
# fullname helper, standard labels — only the rules block is generated.
cat > "$DST" <<EOF
# This file is GENERATED. DO NOT EDIT.
#
# Source of truth: +kubebuilder:rbac:* markers in internal/controller/*.go.
# To change the operator's permissions:
#   1. Edit the relevant +kubebuilder:rbac:groups=...,resources=...,verbs=... marker
#   2. Run 'make manifests' (regenerates config/rbac/role.yaml)
#   3. Run 'make helm-sync-rbac' (regenerates this file)
#
# CI's 'make check-drift' fails if these are out of sync.
{{ if and .Values.rbac.create (eq (include "neo4j-operator.createClusterRole" .) "true") -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "neo4j-operator.fullname" . }}-manager-role
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
rules:
${CORE_RULES}
{{- if .Values.rbac.externalSecretsIntegration }}
${EXT_SECRETS_RULES}
{{- end }}
{{- end }}
EOF

echo "Wrote $DST from $SRC"
