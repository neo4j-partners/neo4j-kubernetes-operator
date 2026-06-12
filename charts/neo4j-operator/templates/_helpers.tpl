{{/*
Expand the name of the chart.
*/}}
{{- define "neo4j-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "neo4j-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "neo4j-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "neo4j-operator.labels" -}}
helm.sh/chart: {{ include "neo4j-operator.chart" . }}
{{ include "neo4j-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "neo4j-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "neo4j-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "neo4j-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "neo4j-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Get the namespace for leader election
*/}}
{{- define "neo4j-operator.leaderElectionNamespace" -}}
{{/* Always the release namespace: controller-runtime puts the lease in the
     pod's own namespace and the binary exposes no flag to move it, so a
     configurable value here only relocated the RBAC away from the lease. */}}
{{- .Release.Namespace }}
{{- end }}

{{/*
Get the watch namespace configuration
*/}}
{{- define "neo4j-operator.watchNamespace" -}}
{{- if eq .Values.operatorMode "namespace" }}
{{- .Release.Namespace }}
{{- else if eq .Values.operatorMode "namespaces" }}
{{- join "," .Values.watchNamespaces }}
{{- else }}
{{- "" }}
{{- end }}
{{- end }}

{{/*
Whether every watchNamespaces entry is a plain (static) namespace name, i.e.
none is a pattern. MUST mirror the operator's own pattern detection in
cmd/main.go (parseWatchNamespaceConfig / hasGlobChars): the "glob:", "regex:",
"re:", "label:" prefixes (case-insensitive) and the bare glob metacharacters
"*", "?", "[". If the template's notion of "static" diverged from the
operator's, the chart could lay down per-namespace Roles for a config the
operator then tries to resolve via cluster-scoped namespace discovery — a
broken install. Returns "true"/"false". An empty list is vacuously static.
*/}}
{{- define "neo4j-operator.watchNamespacesStatic" -}}
{{- $static := true -}}
{{- range .Values.watchNamespaces -}}
{{- $lower := lower (toString .) -}}
{{- if or (hasPrefix "glob:" $lower) (hasPrefix "regex:" $lower) (hasPrefix "re:" $lower) (hasPrefix "label:" $lower) (contains "*" $lower) (contains "?" $lower) (contains "[" $lower) -}}
{{- $static = false -}}
{{- end -}}
{{- end -}}
{{- $static -}}
{{- end }}

{{/*
Whether to use per-namespace Roles (and therefore suppress the manager
ClusterRole + ClusterRoleBinding). True only for: rbac.create +
operatorMode=namespaces + rbac.perNamespaceRoles + a non-empty, STATIC
watchNamespaces list. Returns "true"/"false".

Fails the render (so `helm install` aborts with a clear message) when
perNamespaceRoles is requested but the preconditions can't hold — an empty
list (no namespaces to scope to, and suppressing the ClusterRole would leave
the operator with zero manager permissions) or a pattern entry (pattern
discovery needs a cluster-scoped namespace list/watch a Role can't grant).
Failing fast is deliberate: someone who set perNamespaceRoles=true to forbid
ClusterRoles must never be silently handed one.
*/}}
{{/*
Validate operatorMode up front: a typo (e.g. "Cluster") previously fell through
every mode gate and rendered a ZERO-RBAC, watch-everything install.
*/}}
{{- define "neo4j-operator.validateMode" -}}
{{- if not (has .Values.operatorMode (list "cluster" "namespace" "namespaces")) }}
{{- fail (printf "operatorMode must be one of cluster|namespace|namespaces, got %q" .Values.operatorMode) }}
{{- end }}
{{- if and .Values.rbac.perNamespaceRoles (ne .Values.operatorMode "namespaces") }}
{{- fail "rbac.perNamespaceRoles=true requires operatorMode=namespaces — refusing to silently emit a ClusterRole for an install that asked for namespace-scoped RBAC" }}
{{- end }}
{{- end }}

{{- define "neo4j-operator.perNamespaceRoles" -}}
{{- if and .Values.rbac.create (eq .Values.operatorMode "namespaces") .Values.rbac.perNamespaceRoles -}}
{{- if empty .Values.watchNamespaces -}}
{{- fail "rbac.perNamespaceRoles=true requires a non-empty watchNamespaces list (the namespaces to scope the per-namespace Roles to). Set watchNamespaces, or set rbac.perNamespaceRoles=false to use the ClusterRole." -}}
{{- else if ne (include "neo4j-operator.watchNamespacesStatic" .) "true" -}}
{{- fail "rbac.perNamespaceRoles=true requires a static watchNamespaces list (plain namespace names only). A glob/regex/label/prefix pattern needs a ClusterRole because matching namespaces are discovered via a cluster-scoped namespace list/watch, which a namespaced Role cannot grant. List the namespaces explicitly, or set rbac.perNamespaceRoles=false to use the ClusterRole." -}}
{{- else -}}
true
{{- end -}}
{{- else -}}
false
{{- end -}}
{{- end }}

{{/*
Determine if ClusterRole should be created. The manager ClusterRole is used for
cluster scope and for namespaces scope EXCEPT when per-namespace Roles are in
effect (a static list with rbac.perNamespaceRoles=true), where it is suppressed
in favour of one Role per namespace.
*/}}
{{- define "neo4j-operator.createClusterRole" -}}
{{- if and .Values.rbac.create (or (eq .Values.operatorMode "cluster") (eq .Values.operatorMode "namespaces")) -}}
{{- if eq (include "neo4j-operator.perNamespaceRoles" .) "true" -}}
false
{{- else -}}
true
{{- end -}}
{{- else -}}
false
{{- end -}}
{{- end }}

{{/*
Get the image tag
*/}}
{{- define "neo4j-operator.imageTag" -}}
{{- if .Values.image.tag }}
{{- .Values.image.tag }}
{{- else }}
{{- .Chart.AppVersion }}
{{- end }}
{{- end }}

{{/*
Get the operator image
*/}}
{{- define "neo4j-operator.image" -}}
{{- printf "%s:%s" .Values.image.repository (include "neo4j-operator.imageTag" .) }}
{{- end }}

{{/*
Get container args based on configuration
*/}}
{{- define "neo4j-operator.args" -}}
- --leader-elect={{ .Values.leaderElection.enabled }}
{{- if .Values.developmentMode }}
- --mode=dev
- --zap-devel=true
{{- end }}
- --zap-log-level={{ .Values.logLevel }}
{{- if .Values.metrics.enabled }}
- --metrics-bind-address=:{{ .Values.metrics.service.port }}
{{- if .Values.metrics.secure }}
- --metrics-secure=true
{{- end }}
{{- else }}
- --metrics-bind-address=0
{{- end }}
- --health-probe-bind-address=:8081

{{- end }}
