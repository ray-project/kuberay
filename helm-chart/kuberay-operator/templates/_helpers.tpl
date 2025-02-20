{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "kuberay-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kuberay-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kuberay-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "kuberay-operator.labels" -}}
app.kubernetes.io/name: {{ include "kuberay-operator.name" . }}
helm.sh/chart: {{ include "kuberay-operator.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "kuberay-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "kuberay-operator.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}


{{/*
FeatureGates
*/}}
{{- define "kuberay.featureGates" -}}
{{- $features := "" }}
{{- range .Values.featureGates }}
  {{- $str := printf "%s=%t," .name .enabled }}
  {{- $features = print $features $str }}
{{- end }}
{{- with .Values.featureGates }}
--feature-gates={{ $features | trimSuffix "," }}
{{- end }}
{{- end }}


{{/*
Create a template to ensure consistency for Role and ClusterRole.
*/}}
{{- define "role.consistentRules" -}}
rules:
- apiGroups:
  - ""
  resources:
  - endpoints
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - ""
  resources:
  - events
  - pods/status
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
  - ""
  resources:
  - pods
  verbs:
  - create
  - delete
  - deletecollection
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - ""
  resources:
  - pods/proxy
  - services/status
  verbs:
  - get
  - patch
  - update
- apiGroups:
  - ""
  resources:
  - serviceaccounts
  verbs:
  - create
  - delete
  - get
  - list
  - watch
- apiGroups:
  - ""
  resources:
  - services/proxy
  verbs:
  - create
  - get
  - patch
  - update
- apiGroups:
  - batch
  resources:
  - jobs
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - coordination.k8s.io
  resources:
  - leases
  verbs:
  - create
  - get
  - list
  - update
- apiGroups:
  - extensions
  - networking.k8s.io
  resources:
  - ingresses
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
  - ingressclasses
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - ray.io
  resources:
  - rayclusters
  - rayjobs
  - rayservices
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - ray.io
  resources:
  - rayclusters/finalizers
  - rayjobs/finalizers
  - rayservices/finalizers
  verbs:
  - update
- apiGroups:
  - ray.io
  resources:
  - rayclusters/status
  - rayjobs/status
  - rayservices/status
  verbs:
  - get
  - patch
  - update
- apiGroups:
  - rbac.authorization.k8s.io
  resources:
  - rolebindings
  verbs:
  - create
  - delete
  - get
  - list
  - watch
- apiGroups:
  - rbac.authorization.k8s.io
  resources:
  - roles
  verbs:
  - create
  - delete
  - get
  - list
  - update
  - watch
- apiGroups:
  - route.openshift.io
  resources:
  - routes
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
{{- if or .batchSchedulerEnabled (eq .batchSchedulerName "volcano") }}
- apiGroups:
  - scheduling.volcano.sh
  resources:
  - podgroups
  verbs:
  - create
  - delete
  - get
  - list
  - update
  - watch
- apiGroups:
  - apiextensions.k8s.io
  resources:
  - customresourcedefinitions
  verbs:
  - get
{{- end -}}
{{- end -}}
