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

# Render just the rules array.  yq writes it at zero indent which is the
# correct YAML form under a `rules:` parent key.
RULES=$("$YQ" '.rules' "$SRC")

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
${RULES}
{{- end }}
EOF

echo "Wrote $DST from $SRC"
