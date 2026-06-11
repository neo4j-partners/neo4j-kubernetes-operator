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

# --- Namespaced manager Role ------------------------------------------------
# operatorMode: namespace watches only .Release.Namespace, so the manager
# permissions are granted via a namespaced Role + RoleBinding instead of a
# ClusterRole. This Role MUST carry the SAME rules as the ClusterRole above —
# the manager's cache builds informers (LIST/WATCH) for every reconciled CRD
# AND the label-filtered Job/CronJob/Certificate caches; if any of those is
# missing from the Role, WaitForCacheSync times out and the operator exits at
# startup (issue #199). Generating it from the same CORE_RULES keeps it
# complete and drift-proof — check-drift fails if it falls behind.
#
# Note: a handful of CORE_RULES entries name cluster-scoped resources (nodes,
# cert-manager clusterissuers/issuers, external-secrets cluster*stores). In a
# namespaced Role these are inert — they neither error nor grant anything — so
# features that read them directly (zone-aware scheduling, ClusterIssuer TLS)
# remain a documented namespace-mode limitation (#197), distinct from the
# startup crash this Role fixes.
ROLE_DST="${ROOT}/charts/neo4j-operator/templates/role.yaml"
cat > "$ROLE_DST" <<EOF
# This file is GENERATED. DO NOT EDIT.
#
# Source of truth: +kubebuilder:rbac:* markers in internal/controller/*.go.
# To change the operator's permissions:
#   1. Edit the relevant +kubebuilder:rbac:groups=...,resources=...,verbs=... marker
#   2. Run 'make manifests' (regenerates config/rbac/role.yaml)
#   3. Run 'make helm-sync-rbac' (regenerates this file)
#
# CI's 'make check-drift' fails if these are out of sync.
{{- if and .Values.rbac.create (eq .Values.operatorMode "namespace") }}
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "neo4j-operator.fullname" . }}-manager-role
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
rules:
${CORE_RULES}
{{- if .Values.rbac.externalSecretsIntegration }}
${EXT_SECRETS_RULES}
{{- end }}
{{- end }}
EOF

echo "Wrote $ROLE_DST from $SRC"

# --- Per-namespace manager Roles (operatorMode=namespaces, static list) -----
# When operatorMode=namespaces with a STATIC watchNamespaces list and
# rbac.perNamespaceRoles=true, the operator needs NO cluster-scoped manager
# grants (a static list builds scoped caches and never lists/watches the
# cluster-scoped `namespaces` resource — see cmd/main.go runManagerWithWatchConfig
# !hasPatterns()). So instead of the manager ClusterRole, emit one Role +
# RoleBinding per listed namespace. Same rules as the ClusterRole/role.yaml
# above (CORE_RULES, drift-proof) so they can't fall behind the markers. The
# `neo4j-operator.perNamespaceRoles` helper gates this AND fails the render on a
# pattern/empty list; the gate here is belt-and-suspenders with that helper.
# Cluster-scoped resources named in CORE_RULES (nodes, cluster*issuers,
# cluster*stores) are inert in a namespaced Role — same documented
# namespace-mode limitation as role.yaml (#197).
NS_ROLE_DST="${ROOT}/charts/neo4j-operator/templates/namespaced-roles.yaml"
cat > "$NS_ROLE_DST" <<EOF
# This file is GENERATED. DO NOT EDIT.
#
# Source of truth: +kubebuilder:rbac:* markers in internal/controller/*.go.
# To change the operator's permissions:
#   1. Edit the relevant +kubebuilder:rbac:groups=...,resources=...,verbs=... marker
#   2. Run 'make manifests' (regenerates config/rbac/role.yaml)
#   3. Run 'make helm-sync-rbac' (regenerates this file)
#
# CI's 'make check-drift' fails if these are out of sync.
#
# Emitted only for operatorMode=namespaces + a static watchNamespaces list +
# rbac.perNamespaceRoles=true (the helper fails the render on a pattern/empty
# list). One Role + RoleBinding per listed namespace; no manager ClusterRole.
{{- if eq (include "neo4j-operator.perNamespaceRoles" .) "true" }}
{{- range \$ns := .Values.watchNamespaces }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ include "neo4j-operator.fullname" \$ }}-manager-role
  namespace: {{ \$ns }}
  labels:
    {{- include "neo4j-operator.labels" \$ | nindent 4 }}
rules:
${CORE_RULES}
{{- if \$.Values.rbac.externalSecretsIntegration }}
${EXT_SECRETS_RULES}
{{- end }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "neo4j-operator.fullname" \$ }}-manager-rolebinding
  namespace: {{ \$ns }}
  labels:
    {{- include "neo4j-operator.labels" \$ | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ include "neo4j-operator.fullname" \$ }}-manager-role
subjects:
- kind: ServiceAccount
  name: {{ include "neo4j-operator.serviceAccountName" \$ }}
  namespace: {{ \$.Release.Namespace }}
{{- end }}
{{- end }}
EOF

echo "Wrote $NS_ROLE_DST from $SRC"

# --- Metrics RBAC -----------------------------------------------------------
# controller-runtime's secure metrics endpoint authenticates scrapers via
# TokenReview and authorizes them via SubjectAccessReview. That requires the
# operator's ServiceAccount to hold the metrics-auth role (create
# tokenreviews + subjectaccessreviews); scrapers in turn must be bound to the
# metrics-reader role (GET /metrics). Both are cluster-scoped — the
# authentication.k8s.io / authorization.k8s.io APIs are non-namespaced — so
# they are emitted regardless of operatorMode (unlike the manager role above,
# which is a Role in namespace mode), gated only on secure metrics being on.
#
# Source of truth: config/rbac/metrics_auth_role.yaml + metrics_reader_role.yaml
# (static kustomize bases). The kustomize binding hard-codes the
# controller-manager SA + neo4j-operator-system namespace; the Helm template
# substitutes the chart's serviceAccountName / Release.Namespace instead.
METRICS_DST="${ROOT}/charts/neo4j-operator/templates/metrics-rbac.yaml"
AUTH_SRC="${ROOT}/config/rbac/metrics_auth_role.yaml"
READER_SRC="${ROOT}/config/rbac/metrics_reader_role.yaml"

for f in "$AUTH_SRC" "$READER_SRC"; do
    if [[ ! -f "$f" ]]; then
        echo "error: $f not found." >&2
        exit 1
    fi
done

AUTH_RULES=$("$YQ" '.rules' "$AUTH_SRC")
READER_RULES=$("$YQ" '.rules' "$READER_SRC")

cat > "$METRICS_DST" <<EOF
# This file is GENERATED. DO NOT EDIT.
#
# Source of truth: config/rbac/metrics_auth_role.yaml + metrics_reader_role.yaml.
# To change the metrics RBAC:
#   1. Edit the relevant config/rbac/metrics_*_role.yaml base
#   2. Run 'make helm-sync-rbac' (regenerates this file)
#
# CI's 'make check-drift' fails if this is out of sync.
#
# controller-runtime's secure metrics endpoint (metrics.secure=true) needs the
# operator SA bound to the metrics-auth role so it can run TokenReview /
# SubjectAccessReview on incoming scrapes; scrapers need the metrics-reader
# role. Both ClusterRoles are cluster-scoped (the authn/authz APIs are
# non-namespaced), so they are emitted in every operatorMode.
{{- if and .Values.rbac.create .Values.metrics.enabled .Values.metrics.secure }}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "neo4j-operator.fullname" . }}-metrics-auth-role
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
rules:
${AUTH_RULES}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "neo4j-operator.fullname" . }}-metrics-auth-rolebinding
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "neo4j-operator.fullname" . }}-metrics-auth-role
subjects:
- kind: ServiceAccount
  name: {{ include "neo4j-operator.serviceAccountName" . }}
  namespace: {{ .Release.Namespace }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "neo4j-operator.fullname" . }}-metrics-reader
  labels:
    {{- include "neo4j-operator.labels" . | nindent 4 }}
rules:
${READER_RULES}
{{- end }}
EOF

echo "Wrote $METRICS_DST from $AUTH_SRC + $READER_SRC"
