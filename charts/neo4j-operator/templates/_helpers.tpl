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
{{- if .Values.leaderElection.namespace }}
{{- .Values.leaderElection.namespace }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
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
Determine if ClusterRole should be created
*/}}
{{- define "neo4j-operator.createClusterRole" -}}
{{- if and .Values.rbac.create (or (eq .Values.operatorMode "cluster") (eq .Values.operatorMode "namespaces")) -}}
true
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
{{- else }}
- --metrics-bind-address=0
{{- end }}
- --health-probe-bind-address=:8081
{{- if .Values.webhook.enabled }}
- --webhook-port={{ .Values.webhook.port }}
{{- end }}
{{- end }}
