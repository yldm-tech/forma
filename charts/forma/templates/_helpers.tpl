{{/*
Expand the name of the chart.
This function ensures that the chart name is either taken from `nameOverride` or defaults to `.Chart.Name`.
It also truncates the name to a maximum of 63 characters and removes trailing hyphens.
*/}}
{{- define "forma.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Define the application version to be used in labels.
The version is taken from `.Values.deployment.image.tag` if provided, otherwise it defaults to `.Chart.Version`.
It ensures the version only contains alphanumeric characters, underscores, dots, or hyphens, replacing any invalid characters with a hyphen.
*/}}
{{- define "forma.version" -}}
  {{- $appVersion := default .Chart.Version .Values.deployment.image.tag -}}
  {{- regexReplaceAll "[^a-zA-Z0-9_\\.\\-]" $appVersion "-" | trunc 63 | trimSuffix "-" -}}
{{- end }}


{{/*
Generate a chart name and version string to be used in Helm chart labels.
This follows the format: `<ChartName>-<ChartVersion>`, replacing `+` with `_` and truncating to 63 characters.
*/}}
{{- define "forma.chart" -}}
  {{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}


{{/*
Common labels applied to Kubernetes resources.
These labels help identify and manage the application.
*/}}
{{- define "forma.labels" -}}
helm.sh/chart: {{ include "forma.chart" . }}

# Selector labels
{{ include "forma.selectorLabels" . }}

# Application version label
{{- with include "forma.version" . }}
app.kubernetes.io/version: {{ . | quote }}
{{- end }}

# Managed by Helm
app.kubernetes.io/managed-by: {{ .Release.Service }}

# Part of label, defaults to the chart name if `partOfOverride` is not provided.
app.kubernetes.io/part-of: {{ .Values.partOfOverride | default (include "forma.name" .) }}
{{- end }}


{{/*
Selector labels used for identifying workloads in Kubernetes.
These labels ensure that selectors correctly map to the deployed resources.
*/}}
{{- define "forma.selectorLabels" -}}
app.kubernetes.io/name: {{ include "forma.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: {{ .Values.componentOverride | default (include "forma.name" .) }}
{{- end }}


{{/*
Renders a value that contains a Helm template.
Usage:
{{ include "forma.tplvalues.render" ( dict "value" .Values.path.to.the.Value "context" $) }}
This function allows rendering values dynamically.
*/}}
{{- define "forma.tplvalues.render" -}}
    {{- if typeIs "string" .value }}
        {{- tpl .value .context }}
    {{- else }}
        {{- tpl (.value | toYaml) .context }}
    {{- end }}
{{- end }}

{{/*
Render a Kubernetes EnvVar from chart env maps.
Scalar values become quoted string values. Map values are rendered as EnvVar fields,
which keeps advanced forms such as valueFrom supported.
*/}}
{{- define "forma.envVarValue" -}}
{{- $value := .value -}}
{{- if kindIs "map" $value -}}
{{- include "forma.tplvalues.render" (dict "value" $value "context" .context) -}}
{{- else if kindIs "invalid" $value -}}
value: ""
{{- else -}}
value: {{ include "forma.tplvalues.render" (dict "value" (toString $value) "context" .context) | trim | quote }}
{{- end -}}
{{- end }}

{{- define "forma.envVar" -}}
- name: {{ include "forma.tplvalues.render" (dict "value" .name "context" .context) }}
  {{- include "forma.envVarValue" (dict "value" .value "context" .context) | nindent 2 }}
{{- end }}

{{/*
Default OpenAI-compatible base URL for the bundled vLLM router.
*/}}
{{- define "forma.llmBaseUrl" -}}
{{- if .Values.llm.forma.baseUrl -}}
{{- include "forma.tplvalues.render" (dict "value" .Values.llm.forma.baseUrl "context" .) -}}
{{- else -}}
{{- printf "http://%s-router-service:%s/v1" .Release.Name (toString .Values.llm.routerSpec.servicePort) -}}
{{- end -}}
{{- end }}

{{/*
Allow the release namespace to be overridden.
If `namespaceOverride` is provided, it will be used; otherwise, it defaults to `.Release.Namespace`.
*/}}
{{- define "forma.namespace" -}}
{{- default .Release.Namespace .Values.namespaceOverride -}}
{{- end -}}

{{- define "forma.appSecretName" -}}
{{- printf "%s-app-secrets" (include "forma.name" .) -}}
{{- end }}

{{- define "forma.authzedClusterName" -}}
{{- .Values.authzed.cluster.name | default (printf "%s-spicedb" (include "forma.name" .)) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "forma.authzedManagedSecretName" -}}
{{- printf "%s-authzed" (include "forma.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "forma.authzedAuthSecretName" -}}
{{- .Values.authzed.auth.existingSecret | default (include "forma.authzedManagedSecretName" .) -}}
{{- end }}

{{- define "forma.authzedDatastoreSecretName" -}}
{{- .Values.authzed.datastore.existingSecret | default (include "forma.authzedManagedSecretName" .) -}}
{{- end }}

{{- define "forma.authzedEndpoint" -}}
{{- if .Values.authzed.endpoint -}}
{{- .Values.authzed.endpoint -}}
{{- else if eq .Values.authzed.mode "selfHosted" -}}
{{- printf "%s:50051" (include "forma.authzedClusterName" .) -}}
{{- else if eq .Values.authzed.mode "external" -}}
{{- fail "authzed.endpoint is required when authzed.mode=external" -}}
{{- else -}}
{{- fail "authzed.mode must be one of: selfHosted, external" -}}
{{- end -}}
{{- end }}

{{- define "forma.authzedInsecure" -}}
{{- if eq .Values.authzed.insecure nil -}}
{{- eq .Values.authzed.mode "selfHosted" -}}
{{- else -}}
{{- .Values.authzed.insecure -}}
{{- end -}}
{{- end }}

{{- define "forma.authzedPresharedKey" -}}
{{- /* Cluster-generated credentials are persisted through the managed Secret. Renderers without
      live Secret access must use authzed.auth.existingSecret, as documented in the chart README. */ -}}
{{- $secretName := include "forma.authzedManagedSecretName" . -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace $secretName -}}
{{- $secretData := dig "data" dict $secret -}}
{{- if index $secretData .Values.authzed.auth.tokenKey -}}
{{- index $secretData .Values.authzed.auth.tokenKey | b64dec -}}
{{- else -}}
{{- randAlphaNum 48 -}}
{{- end -}}
{{- end }}

{{- define "forma.authzedDatabasePassword" -}}
{{- /* See forma.authzedPresharedKey for the offline-rendering persistence contract. */ -}}
{{- $secretName := include "forma.authzedManagedSecretName" . -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace $secretName -}}
{{- $secretData := dig "data" dict $secret -}}
{{- if index $secretData "database_password" -}}
{{- index $secretData "database_password" | b64dec -}}
{{- else -}}
{{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{- define "forma.redisName" -}}
{{- .Values.redis.fullnameOverride | default (printf "%s-redis" (include "forma.name" .)) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "forma.redisMasterName" -}}
{{- printf "%s-master" (include "forma.redisName" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "forma.redisHeadlessName" -}}
{{- printf "%s-headless" (include "forma.redisName" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "forma.redisImage" -}}
{{- if .Values.redis.image.digest -}}
{{- printf "%s@%s" .Values.redis.image.repository .Values.redis.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.redis.image.repository .Values.redis.image.tag -}}
{{- end -}}
{{- end }}

{{- define "forma.redisSecretName" -}}
{{- .Values.redis.auth.existingSecret | default (include "forma.appSecretName" .) -}}
{{- end }}

{{- define "forma.redisSecretKey" -}}
{{- .Values.redis.auth.existingSecretPasswordKey | default "REDIS_PASSWORD" -}}
{{- end }}

{{- define "forma.migrationJobName" -}}
{{- printf "%s-migration" (include "forma.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Render the database environment shared by the migration readiness and migration containers.
Keeping this in one helper ensures both containers resolve DATABASE_URL and MIGRATE_DATABASE_URL identically.
*/}}
{{- define "forma.migrationEnvironment" -}}
{{- if or .Values.deployment.envFrom (or (and .Values.externalSecret.enabled (index .Values.externalSecret.files "app-secrets")) .Values.secret.enabled) }}
envFrom:
{{- if or .Values.secret.enabled (and .Values.externalSecret.enabled (index .Values.externalSecret.files "app-secrets")) }}
  - secretRef:
      name: {{ template "forma.name" . }}-app-secrets
{{- end }}
{{- range $value := .Values.deployment.envFrom }}
{{- if (eq .type "configmap") }}
  - configMapRef:
      {{- if .name }}
      name: {{ include "forma.tplvalues.render" ( dict "value" $value.name "context" $ ) }}
      {{- else if .nameSuffix }}
      name: {{ template "forma.name" $ }}-{{ include "forma.tplvalues.render" ( dict "value" $value.nameSuffix "context" $ ) }}
      {{- else }}
      name: {{ template "forma.name" $ }}
      {{- end }}
{{- end }}
{{- if (eq .type "secret") }}
  - secretRef:
      {{- if .name }}
      name: {{ include "forma.tplvalues.render" ( dict "value" $value.name "context" $ ) }}
      {{- else if .nameSuffix }}
      name: {{ template "forma.name" $ }}-{{ include "forma.tplvalues.render" ( dict "value" $value.nameSuffix "context" $ ) }}
      {{- else }}
      name: {{ template "forma.name" $ }}
      {{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- if .Values.deployment.env }}
env:
{{- range $key, $value := .Values.deployment.env }}
  {{- include "forma.envVar" (dict "name" $key "value" $value "context" $) | nindent 2 }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Forma application image reference. A configured digest takes precedence over the tag.
*/}}
{{- define "forma.deploymentImage" -}}
{{- if .Values.deployment.image.digest -}}
{{- printf "%s@%s" .Values.deployment.image.repository .Values.deployment.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.deployment.image.repository (.Values.deployment.image.tag | default .Chart.AppVersion | default "latest") -}}
{{- end -}}
{{- end }}
{{/* Reject Hugging Face tokens that cannot be written into an externally managed auth secret. */}}
{{- define "forma.validateHubEmbeddingsHuggingFaceSecret" -}}
{{- if and .Values.hub.embeddings.auth.existingSecret .Values.hub.embeddings.huggingFace.token (not .Values.hub.embeddings.huggingFace.existingSecret) -}}
{{- fail "hub.embeddings.huggingFace.token cannot be stored when hub.embeddings.auth.existingSecret is set; put HF_TOKEN in the existing auth secret or set hub.embeddings.huggingFace.existingSecret" -}}
{{- end -}}
{{- end }}

{{- define "forma.postgresAdminPassword" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "POSTGRES_ADMIN_PASSWORD" }}
    {{- index $secretData "POSTGRES_ADMIN_PASSWORD" | b64dec -}}
{{- else }}
    {{- randAlphaNum 16 -}}
{{- end -}}
{{- end }}

{{- define "forma.postgresUserPassword" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "POSTGRES_USER_PASSWORD" }}
    {{- index $secretData "POSTGRES_USER_PASSWORD" | b64dec -}}
{{- else }}
    {{- randAlphaNum 16 -}}
{{- end -}}
{{- end }}

{{- define "forma.redisPassword" -}}
{{- $redisSecretName := include "forma.redisSecretName" . }}
{{- $redisSecretKey := include "forma.redisSecretKey" . }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $redisSecretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData $redisSecretKey }}
    {{- index $secretData $redisSecretKey | b64dec -}}
{{- else if eq $redisSecretName (include "forma.appSecretName" .) }}
    {{- randAlphaNum 16 -}}
{{- else }}
    {{- fail (printf "redis.auth.existingSecret %q must already exist in namespace %q and contain %s when secret.enabled=true so REDIS_URL can use the same password as the bundled Valkey server. Disable secret.enabled and provide app-secrets externally, or pre-create the Redis auth secret." $redisSecretName .Release.Namespace $redisSecretKey) -}}
{{- end -}}
{{- end }}

{{- define "forma.cronSecret" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "CRON_SECRET" }}
    {{- index $secretData "CRON_SECRET" | b64dec -}}
{{- else if and $secret (hasKey $secret "data") }}
    {{- fail (printf "Secret %q exists in namespace %q but is missing CRON_SECRET" (include "forma.appSecretName" .) .Release.Namespace) -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{- define "forma.encryptionKey" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "ENCRYPTION_KEY" }}
    {{- index $secretData "ENCRYPTION_KEY" | b64dec -}}
{{- else if and $secret (hasKey $secret "data") }}
    {{- fail (printf "Secret %q exists in namespace %q but is missing ENCRYPTION_KEY" (include "forma.appSecretName" .) .Release.Namespace) -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{- define "forma.nextAuthSecret" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "NEXTAUTH_SECRET" }}
    {{- index $secretData "NEXTAUTH_SECRET" | b64dec -}}
{{- else if and $secret (hasKey $secret "data") }}
    {{- fail (printf "Secret %q exists in namespace %q but is missing NEXTAUTH_SECRET" (include "forma.appSecretName" .) .Release.Namespace) -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}
{{- define "forma.envoy.gatewayClassName" -}}
{{- if .Values.envoy.forma.gatewayClass.name -}}
{{- .Values.envoy.forma.gatewayClass.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s-envoy" .Release.Name (include "forma.namespace" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "forma.envoy.gatewayName" -}}
{{- if .Values.envoy.forma.gateway.name -}}
{{- .Values.envoy.forma.gateway.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-envoy-gateway" (include "forma.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "forma.envoy.proxyName" -}}
{{- if .Values.envoy.forma.proxy.name -}}
{{- .Values.envoy.forma.proxy.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-envoy-proxy" (include "forma.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "forma.envoy.proxyServiceName" -}}
{{- if .Values.envoy.forma.proxy.service.name -}}
{{- .Values.envoy.forma.proxy.service.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-envoy" (include "forma.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end }}

{{- define "forma.envoy.ingressHost" -}}
{{- if .Values.envoy.forma.ingress.host -}}
{{- tpl .Values.envoy.forma.ingress.host $ -}}
{{- else if and .Values.ingress.hosts (gt (len .Values.ingress.hosts) 0) -}}
{{- tpl (index .Values.ingress.hosts 0).host $ -}}
{{- end -}}
{{- end }}

{{- define "forma.envoy.defaultRedisUrl" -}}
{{- printf "%s-master:6379" .Values.envoyRedis.fullnameOverride -}}
{{- end }}
