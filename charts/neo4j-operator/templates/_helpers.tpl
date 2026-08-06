{{/*
Expand the name of the chart.
*/}}
{{- define "neo4j-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
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

{{- define "neo4j-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "neo4j-operator.labels" -}}
helm.sh/chart: {{ include "neo4j-operator.chart" . }}
{{ include "neo4j-operator.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end }}

{{- define "neo4j-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "neo4j-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "neo4j-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default "neo4j-operator-controller-manager" .Values.serviceAccount.name }}
{{- else }}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "neo4j-operator.watchNamespacesCSV" -}}
{{- join "," .Values.watchNamespaces }}
{{- end }}

{{/*
Manager Role rules — keep in sync with config/rbac/role.yaml
*/}}
{{- define "neo4j-operator.managerRoleRules" -}}
rules:
- apiGroups:
  - ""
  resources:
  - configmaps
  - endpoints
  - persistentvolumeclaims
  - secrets
  - serviceaccounts
  - services
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - rbac.authorization.k8s.io
  resources:
  - rolebindings
  - roles
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - create
  - patch
- apiGroups:
  - apps
  resources:
  - statefulsets
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - policy
  resources:
  - poddisruptionbudgets
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - networking.k8s.io
  resources:
  - networkpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - neo4j.com
  resources:
  - neo4js
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - neo4j.com
  resources:
  - neo4js/finalizers
  verbs:
  - update
- apiGroups:
  - neo4j.com
  resources:
  - neo4js/status
  verbs:
  - get
  - patch
  - update
- apiGroups:
  - monitoring.coreos.com
  resources:
  - servicemonitors
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
{{- end }}
