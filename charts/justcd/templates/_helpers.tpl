{{- define "justcd.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 54 | trimSuffix "-" -}}
{{- end -}}

{{- define "justcd.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 54 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "justcd.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 54 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 54 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "justcd.labels" -}}
app.kubernetes.io/name: {{ include "justcd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "justcd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "justcd.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}
