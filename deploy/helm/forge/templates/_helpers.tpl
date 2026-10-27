{{- define "forge.labels" -}}
app.kubernetes.io/part-of: forge
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "forge.redisUrl" -}}
{{- if .Values.redis.enabled -}}
redis://{{ .Release.Name }}-redis:6379/0
{{- else -}}
{{ required "redis.externalUrl is required when redis.enabled=false" .Values.redis.externalUrl }}
{{- end -}}
{{- end -}}
