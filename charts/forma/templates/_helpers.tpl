{{/*
Expand the name of the chart.
This function ensures that the chart name is either taken from `nameOverride` or defaults to `.Chart.Name`.
It also truncates the name to a maximum of 63 characters and removes trailing hyphens.
*/}}
{{- define "forma.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Hub resource name: base name truncated to 59 chars then "-hub" so the suffix is never lost (63 char limit).
*/}}
{{- define "forma.hubname" -}}
{{- $base := include "forma.name" . | trunc 59 | trimSuffix "-" }}
{{- printf "%s-hub" $base | trimSuffix "-" }}
{{- end }}

{{/*
Cube.js resource name.
*/}}
{{- define "forma.cubeName" -}}
{{- $base := include "forma.name" . | trunc 58 | trimSuffix "-" }}
{{- printf "%s-cube" $base | trimSuffix "-" }}
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

{{- define "forma.hubSecretName" -}}
{{- default (include "forma.appSecretName" .) .Values.hub.existingSecret -}}
{{- end }}

{{- define "forma.hubMigrationWaitServiceAccountName" -}}
{{- printf "%s-migration-wait" (include "forma.hubname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Hub image reference. Pin by digest in production (hub.image.digest = "sha256:..."); falls back to
hub.image.tag for local/dev. All Hub workloads (deployment, init container, migration job, future
hub-worker) must use this helper so they cannot drift apart.
*/}}
{{- define "forma.hubImage" -}}
{{- if .Values.hub.image.digest -}}
{{- printf "%s@%s" .Values.hub.image.repository .Values.hub.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.hub.image.repository (.Values.hub.image.tag | default "latest") -}}
{{- end -}}
{{- end }}

{{/*
Hub worker resource name.
*/}}
{{- define "forma.hubWorkerName" -}}
{{- $base := include "forma.name" . | trunc 52 | trimSuffix "-" }}
{{- printf "%s-hub-worker" $base | trimSuffix "-" }}
{{- end }}

{{/*
Taxonomy service resource name.
*/}}
{{- define "forma.taxonomyName" -}}
{{- $base := include "forma.name" . | trunc 54 | trimSuffix "-" }}
{{- printf "%s-taxonomy" $base | trimSuffix "-" }}
{{- end }}

{{/*
Taxonomy service image reference. A configured digest takes precedence over the tag.
*/}}
{{- define "forma.taxonomyImage" -}}
{{- if .Values.taxonomy.image.digest -}}
{{- printf "%s@%s" .Values.taxonomy.image.repository .Values.taxonomy.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.taxonomy.image.repository (.Values.taxonomy.image.tag | default "latest") -}}
{{- end -}}
{{- end }}

{{- define "forma.taxonomyManagedSecretName" -}}
{{- printf "%s-secret" (include "forma.taxonomyName" .) -}}
{{- end }}

{{- define "forma.taxonomyAuthSecretName" -}}
{{- default (include "forma.taxonomyManagedSecretName" .) .Values.taxonomy.auth.existingSecret -}}
{{- end }}

{{- define "forma.taxonomyLlmSecretName" -}}
{{- default (include "forma.taxonomyManagedSecretName" .) .Values.taxonomy.llm.existingSecret -}}
{{- end }}

{{- define "forma.taxonomyVertexSecretName" -}}
{{- default (include "forma.taxonomyManagedSecretName" .) .Values.taxonomy.llm.vertex.existingSecret -}}
{{- end }}

{{- define "forma.taxonomyServiceUrl" -}}
{{- printf "http://%s:%v" (include "forma.taxonomyName" .) (.Values.taxonomy.service.port | default .Values.taxonomy.port) -}}
{{- end }}

{{- define "forma.taxonomyLlmBaseUrl" -}}
{{- if .Values.taxonomy.llm.baseUrl -}}
{{- include "forma.tplvalues.render" (dict "value" .Values.taxonomy.llm.baseUrl "context" .) -}}
{{- else if .Values.llm.enabled -}}
{{- include "forma.llmBaseUrl" . -}}
{{- else -}}
{{- "" -}}
{{- end -}}
{{- end }}

{{- define "forma.taxonomyServiceToken" -}}
{{- $secretName := include "forma.taxonomyManagedSecretName" . }}
{{- $secretKey := .Values.taxonomy.auth.serviceTokenKey | default "TAXONOMY_SERVICE_TOKEN" }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $secretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData $secretKey }}
    {{- index $secretData $secretKey | b64dec -}}
{{- else if .Values.taxonomy.auth.serviceToken }}
    {{- .Values.taxonomy.auth.serviceToken -}}
{{- else }}
    {{- randAlphaNum 48 -}}
{{- end -}}
{{- end }}

{{- define "forma.taxonomyHubInternalApiToken" -}}
{{- $secretName := include "forma.taxonomyManagedSecretName" . }}
{{- $secretKey := .Values.taxonomy.auth.hubInternalApiTokenKey | default "HUB_INTERNAL_API_TOKEN" }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $secretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData $secretKey }}
    {{- index $secretData $secretKey | b64dec -}}
{{- else if .Values.taxonomy.auth.hubInternalApiToken }}
    {{- .Values.taxonomy.auth.hubInternalApiToken -}}
{{- else }}
    {{- randAlphaNum 48 -}}
{{- end -}}
{{- end }}

{{- define "forma.taxonomyLlmApiKey" -}}
{{- $secretName := include "forma.taxonomyManagedSecretName" . }}
{{- $secretKey := .Values.taxonomy.llm.apiKeySecretKey | default "TAXONOMY_LLM_API_KEY" }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $secretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData $secretKey }}
    {{- index $secretData $secretKey | b64dec -}}
{{- else if .Values.taxonomy.llm.apiKey }}
    {{- .Values.taxonomy.llm.apiKey -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{- define "forma.taxonomyVertexCredentialsJson" -}}
{{- $secretName := include "forma.taxonomyManagedSecretName" . }}
{{- $secretKey := .Values.taxonomy.llm.vertex.credentialsJsonSecretKey | default "TAXONOMY_GOOGLE_CLOUD_CREDENTIALS_JSON" }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $secretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if .Values.taxonomy.llm.vertex.credentialsJson }}
    {{- .Values.taxonomy.llm.vertex.credentialsJson -}}
{{- else if index $secretData $secretKey }}
    {{- index $secretData $secretKey | b64dec -}}
{{- else }}
    {{- "" -}}
{{- end -}}
{{- end }}

{{/*
Hub env managed by taxonomy when the optional taxonomy service is enabled.
*/}}
{{- define "forma.taxonomyHubEnv" -}}
{{- $root := .root -}}
{{- if and $root.Values.taxonomy.enabled $root.Values.taxonomy.autoConfigureHub }}
- name: TAXONOMY_SERVICE_URL
  value: {{ include "forma.taxonomyServiceUrl" $root | quote }}
- name: TAXONOMY_SERVICE_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "forma.taxonomyAuthSecretName" $root }}
      key: {{ $root.Values.taxonomy.auth.serviceTokenKey | default "TAXONOMY_SERVICE_TOKEN" }}
- name: HUB_INTERNAL_API_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "forma.taxonomyAuthSecretName" $root }}
      key: {{ $root.Values.taxonomy.auth.hubInternalApiTokenKey | default "HUB_INTERNAL_API_TOKEN" }}
- name: TAXONOMY_STUCK_RUN_TIMEOUT_SECONDS
  value: {{ $root.Values.taxonomy.hubStaleRunTimeoutSeconds | quote }}
- name: TAXONOMY_REAPER_INTERVAL_SECONDS
  value: {{ $root.Values.taxonomy.hubReaperIntervalSeconds | quote }}
{{- end }}
{{- end }}

{{/*
Returns true when an env var is managed by taxonomy auto-configuration and should not be rendered from hub.env.
*/}}
{{- define "forma.taxonomyHubEnvManaged" -}}
{{- $key := .key -}}
{{- if has $key (list "TAXONOMY_SERVICE_URL" "TAXONOMY_SERVICE_TOKEN" "HUB_INTERNAL_API_TOKEN" "TAXONOMY_STUCK_RUN_TIMEOUT_SECONDS" "TAXONOMY_REAPER_INTERVAL_SECONDS") -}}
true
{{- end -}}
{{- end }}

{{/*
Returns true when an env var is managed by the taxonomy deployment and should not be rendered from taxonomy.env.
*/}}
{{- define "forma.taxonomyEnvManaged" -}}
{{- $key := .key -}}
{{- if has $key (list "APP_ENV" "HUB_INTERNAL_API_URL" "HUB_INTERNAL_API_TOKEN" "TAXONOMY_SERVICE_TOKEN" "TAXONOMY_LLM_PROVIDER" "TAXONOMY_LLM_MODEL" "TAXONOMY_LLM_BASE_URL" "TAXONOMY_LLM_API_KEY" "TAXONOMY_VERTEX_PROJECT" "TAXONOMY_VERTEX_LOCATION" "TAXONOMY_GOOGLE_CLOUD_CREDENTIALS_JSON" "TAXONOMY_VERTEX_THINKING_BUDGET" "TAXONOMY_LLM_TEMPERATURE" "TAXONOMY_LLM_STRUCTURED_OUTPUT_MODE" "TAXONOMY_LLM_CONTEXT_WINDOW_TOKENS" "TAXONOMY_LLM_LABEL_MAX_TOKENS" "TAXONOMY_LLM_TREE_MAX_TOKENS" "TAXONOMY_LLM_PROMPT_TOKEN_RESERVE" "TAXONOMY_LLM_PROVIDER_MAX_ATTEMPTS" "TAXONOMY_LLM_MAX_ATTEMPTS" "TAXONOMY_LLM_TIMEOUT_SECONDS" "HUB_CLIENT_TIMEOUT_SECONDS" "HUB_CLIENT_MAX_ATTEMPTS" "HUB_HEARTBEAT_INTERVAL_SECONDS" "TAXONOMY_RUN_TIMEOUT_SECONDS" "TAXONOMY_EMBEDDING_DIMENSION" "TAXONOMY_MIN_EMBEDDED_RECORDS" "TAXONOMY_MAX_RECORDS" "TAXONOMY_MAX_CLUSTERS" "TAXONOMY_RANDOM_SEED") -}}
true
{{- end -}}
{{- end }}

{{/*
Hub embeddings runtime resource name.
*/}}
{{- define "forma.hubEmbeddingsName" -}}
{{- $base := include "forma.name" . | trunc 48 | trimSuffix "-" }}
{{- printf "%s-hub-embeddings" $base | trimSuffix "-" }}
{{- end }}

{{/* Worker-only background embeddings runtime resource name. */}}
{{- define "forma.hubEmbeddingsBackgroundName" -}}
{{- $base := include "forma.name" . | trunc 37 | trimSuffix "-" }}
{{- printf "%s-hub-embeddings-background" $base | trimSuffix "-" }}
{{- end }}

{{/* Headless service for stable background StatefulSet identities. */}}
{{- define "forma.hubEmbeddingsBackgroundHeadlessName" -}}
{{- printf "%s-headless" (include "forma.hubEmbeddingsBackgroundName" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Secret used by Hub and the embeddings runtime for the embeddings API key.
*/}}
{{- define "forma.hubEmbeddingsSecretName" -}}
{{- default (printf "%s-secret" (include "forma.hubEmbeddingsName" .)) .Values.hub.embeddings.auth.existingSecret -}}
{{- end }}

{{/*
Secret used by the embeddings runtime for Hugging Face access.
*/}}
{{- define "forma.hubEmbeddingsHuggingFaceSecretName" -}}
{{- default (include "forma.hubEmbeddingsSecretName" .) .Values.hub.embeddings.huggingFace.existingSecret -}}
{{- end }}

{{/* Reject Hugging Face tokens that cannot be written into an externally managed auth secret. */}}
{{- define "forma.validateHubEmbeddingsHuggingFaceSecret" -}}
{{- if and .Values.hub.embeddings.auth.existingSecret .Values.hub.embeddings.huggingFace.token (not .Values.hub.embeddings.huggingFace.existingSecret) -}}
{{- fail "hub.embeddings.huggingFace.token cannot be stored when hub.embeddings.auth.existingSecret is set; put HF_TOKEN in the existing auth secret or set hub.embeddings.huggingFace.existingSecret" -}}
{{- end -}}
{{- end }}

{{/*
Model name Hub sends to the OpenAI-compatible embeddings endpoint.
*/}}
{{- define "forma.hubEmbeddingsServedModelName" -}}
{{- default .Values.hub.embeddings.model .Values.hub.embeddings.servedModelName -}}
{{- end }}

{{/*
OpenAI-compatible embeddings base URL used by Hub.
*/}}
{{- define "forma.hubEmbeddingsBaseURL" -}}
{{- if .Values.hub.embeddings.baseUrl -}}
{{- .Values.hub.embeddings.baseUrl -}}
{{- else -}}
{{- printf "http://%s:%v/v1" (include "forma.hubEmbeddingsName" .) (.Values.hub.embeddings.service.port | default .Values.hub.embeddings.port) -}}
{{- end -}}
{{- end }}

{{/* Worker-only OpenAI-compatible background embeddings base URL. */}}
{{- define "forma.hubEmbeddingsBackgroundBaseURL" -}}
{{- if .Values.hub.embeddings.background.baseUrl -}}
{{- .Values.hub.embeddings.background.baseUrl -}}
{{- else -}}
{{- printf "http://%s:%v/v1" (include "forma.hubEmbeddingsBackgroundName" .) (.Values.hub.embeddings.background.service.port | default .Values.hub.embeddings.port) -}}
{{- end -}}
{{- end }}

{{/*
Embedding API key value for the generated embeddings secret.
*/}}
{{- define "forma.hubEmbeddingsApiKey" -}}
{{- $secretName := include "forma.hubEmbeddingsSecretName" . }}
{{- $secretKey := .Values.hub.embeddings.auth.secretKey | default "EMBEDDING_PROVIDER_API_KEY" }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $secretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData $secretKey }}
    {{- index $secretData $secretKey | b64dec -}}
{{- else if .Values.hub.embeddings.auth.apiKey }}
    {{- .Values.hub.embeddings.auth.apiKey -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{/*
Shared Hub embedding env. These values are managed from hub.embeddings when the
self-hosted runtime is enabled so Hub API and Hub worker cannot drift.
*/}}
{{- define "forma.hubEmbeddingEnv" -}}
{{- $root := .root -}}
{{- $worker := .worker | default false -}}
{{- $env := .env | default (dict) -}}
{{- if $root.Values.hub.embeddings.enabled }}
- name: EMBEDDING_PROVIDER
  value: "openai"
- name: EMBEDDING_MODEL
  value: {{ include "forma.hubEmbeddingsServedModelName" $root | quote }}
- name: EMBEDDING_BASE_URL
  {{- if and $worker $root.Values.hub.embeddings.background.enabled }}
  value: {{ include "forma.hubEmbeddingsBackgroundBaseURL" $root | quote }}
  {{- else }}
  value: {{ include "forma.hubEmbeddingsBaseURL" $root | quote }}
  {{- end }}
- name: EMBEDDING_PROVIDER_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "forma.hubEmbeddingsSecretName" $root }}
      key: {{ $root.Values.hub.embeddings.auth.secretKey | default "EMBEDDING_PROVIDER_API_KEY" }}
- name: EMBEDDING_MAX_CONCURRENT
  {{- if and $worker $root.Values.hub.embeddings.background.enabled }}
  value: {{ $root.Values.hub.embeddings.background.maxConcurrent | quote }}
  {{- else }}
  value: {{ $root.Values.hub.embeddings.maxConcurrent | quote }}
  {{- end }}
- name: EMBEDDING_NORMALIZE
  value: {{ $root.Values.hub.embeddings.normalize | quote }}
{{- if $worker }}
- name: EMBEDDING_BATCH_SIZE
  value: {{ ternary $root.Values.hub.embeddings.background.batchSize "1" $root.Values.hub.embeddings.background.enabled | quote }}
- name: EMBEDDING_BATCH_MAX_WAIT_MS
  value: {{ ternary $root.Values.hub.embeddings.background.batchMaxWaitMs "25" $root.Values.hub.embeddings.background.enabled | quote }}
- name: EMBEDDING_BATCH_MAX_IN_FLIGHT
  value: {{ ternary $root.Values.hub.embeddings.background.batchMaxInFlight "1" $root.Values.hub.embeddings.background.enabled | quote }}
- name: EMBEDDING_HTTP_DISABLE_KEEP_ALIVES
  {{- if hasKey $env "EMBEDDING_HTTP_DISABLE_KEEP_ALIVES" }}
  value: {{ index $env "EMBEDDING_HTTP_DISABLE_KEEP_ALIVES" | quote }}
  {{- else }}
  value: {{ ternary $root.Values.hub.embeddings.background.httpDisableKeepAlives "false" $root.Values.hub.embeddings.background.enabled | quote }}
  {{- end }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Returns true when an env var is managed by hub.embeddings and should not be rendered from hub.env/worker.env.
*/}}
{{- define "forma.hubEmbeddingEnvManaged" -}}
{{- $key := .key -}}
{{- if has $key (list "EMBEDDING_PROVIDER" "EMBEDDING_MODEL" "EMBEDDING_BASE_URL" "EMBEDDING_PROVIDER_API_KEY" "EMBEDDING_MAX_CONCURRENT" "EMBEDDING_NORMALIZE" "EMBEDDING_BATCH_SIZE" "EMBEDDING_BATCH_MAX_WAIT_MS" "EMBEDDING_BATCH_MAX_IN_FLIGHT" "EMBEDDING_HTTP_DISABLE_KEEP_ALIVES") -}}
true
{{- end -}}
{{- end }}

{{/* Name of one deliberate embedding backfill run. */}}
{{- define "forma.hubEmbeddingBackfillName" -}}
{{- $runID := regexReplaceAll "[^a-z0-9-]+" (.Values.hub.embeddingBackfill.runId | lower) "-" | trunc 20 | trimAll "-" -}}
{{- printf "%s-embedding-backfill-%s" (include "forma.hubname" . | trunc 22 | trimSuffix "-") $runID | trunc 63 | trimSuffix "-" -}}
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

{{- define "forma.hubApiKey" -}}
{{- $hubSecretName := include "forma.hubSecretName" . }}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace $hubSecretName) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "HUB_API_KEY" }}
    {{- index $secretData "HUB_API_KEY" | b64dec -}}
{{- else if .Values.hub.existingSecret }}
    {{- fail (printf "hub.existingSecret %q must already exist in namespace %q and contain HUB_API_KEY when rendering the generated app secret. Disable secret.enabled and provide app-secrets externally, or pre-create the Hub secret." $hubSecretName .Release.Namespace) -}}
{{- else }}
    {{- randAlphaNum 32 -}}
{{- end -}}
{{- end }}

{{- define "forma.cubejsApiSecret" -}}
{{- $secret := (lookup "v1" "Secret" .Release.Namespace (include "forma.appSecretName" .)) }}
{{- $secretData := dig "data" dict $secret }}
{{- if index $secretData "CUBEJS_API_SECRET" }}
    {{- index $secretData "CUBEJS_API_SECRET" | b64dec -}}
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
