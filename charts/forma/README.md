# forma

![Version: 5.3.4](https://img.shields.io/badge/Version-5.3.4-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 5.3.4](https://img.shields.io/badge/AppVersion-5.3.4-informational?style=flat-square)

A Helm chart for Forma with PostgreSQL, Valkey

**Homepage:** <https://forma.ylam.ai/docs/self-hosting/setup/kubernetes>

The version badges describe the latest published OCI chart. The source `Chart.yaml` keeps the development chart
version at `0.0.0-dev`; the release workflow stamps the requested chart version into the packaged artifact.

## Maintainers

| Name       | Email                 | Url |
| ---------- | --------------------- | --- |
| Forma | <info@forma.ylam.ai> |     |

## Requirements

| Repository                                      | Name         | Version |
| ----------------------------------------------- | ------------ | ------- |
| oci://registry-1.docker.io/bitnamicharts        | postgresql   | 16.4.16 |
| oci://docker.io/envoyproxy                      | gateway-helm | v1.7.1  |
| oci://registry-1.docker.io/bitnamicharts        | envoyRedis   | 20.11.2 |
| https://vllm-project.github.io/production-stack | vllm-stack   | 0.1.11  |

## Envoy bundle modes

The chart can optionally deploy Forma behind Envoy Gateway with a dedicated Redis HA backend for Envoy global
rate limiting.

- `envoy.enabled=true` enables the app-bound Envoy resources such as `Gateway`, `HTTPRoute`, and rate-limit policies.
- `envoy.controller.enabled=true` installs a bundled Envoy Gateway controller with the release.
- `envoy.controller.enabled=false` keeps the chart in external-controller mode and assumes the cluster already has
  Gateway API CRDs plus an Envoy Gateway controller compatible with
  `envoy.config.envoyGateway.gateway.controllerName`.
- `envoyRedis.enabled=true` deploys a dedicated Redis replication + Sentinel bundle for Envoy RLS. It is intentionally
  separate from the bundled app Valkey deployment.
- The bundled controller reads its Redis backend from `envoy.config.envoyGateway.rateLimit.backend.redis.url`.
  If you enable Redis authentication or override `envoyRedis.fullnameOverride`, set that URL explicitly so the
  controller points at the correct backend.
- OpenTelemetry proxy metrics can target either `envoy.forma.proxy.telemetry.openTelemetry.host`/`port`
  or `backendRefs`, but not both. Prefer `host`/`port` for collectors that live in another namespace.
- When both the main app ingress and the Envoy API ingress are enabled, set `envoy.forma.ingress.host`
  explicitly so the Envoy host choice is intentional.

The intended defaults are:

- self-hosted / single-tenant clusters: bundled controller mode
- shared clusters with an existing platform controller: external-controller mode

The chart leaves both `ingress.enabled` and `envoy.enabled` disabled because ingress and gateway choices are
cluster-specific. Do not expose Forma v5 directly with those defaults: enable the chart-managed Envoy path
or provide equivalent edge rate limiting for the documented route coverage. The default
`autoscaling.minReplicas: 1` and `pdb.minAvailable: 1` are also a quick-start combination; raise the minimum to at
least two for availability during voluntary disruptions, or change/disable the PDB for an intentional
single-replica deployment.

## AuthZed / SpiceDB

Forma v6 enables AuthZed, `fully_consistent` authorization, and the bundled SpiceDB operator by default.

### Breaking changes from v5

| Setting                    | v5 default | v6 default | Existing shared-operator clusters                                              |
| -------------------------- | ---------- | ---------- | ------------------------------------------------------------------------------- |
| `authzed.operator.install` | `false`    | `true`     | Set `authzed.operator.install=false` before upgrading to avoid duplicate reconcilers. |

For a cluster where a compatible operator already watches the Forma namespace:

```yaml
authzed:
  operator:
    install: false
```

The default installs the pinned SpiceDB operator, creates a two-replica `SpiceDBCluster`, and creates a dedicated
`spicedb` database and role in the bundled PostgreSQL server. During normal Helm installs and upgrades, the
chart reuses generated credentials from the existing cluster Secret. Renderers without live Secret access,
including offline `helm template` and Argo CD manifest generation, must provide persistent credentials through
`authzed.auth.existingSecret` and `authzed.datastore.existingSecret`; otherwise generated values are not stable
between renders. The operator runs datastore migrations before rolling out SpiceDB.

The bundled PostgreSQL dependency uses the following resource baseline, which was validated while PostgreSQL
also served SpiceDB:

```yaml
postgresql:
  primary:
    resources:
      requests:
        cpu: 250m
        memory: 512Mi
      limits:
        cpu: "1"
        memory: 1Gi
```

Helm cannot condition values passed to the PostgreSQL dependency on a sibling value, so the safe database
baseline remains in effect. Override `authzed.cluster.resources` and `postgresql.primary.resources` to match the
expected authorization traffic and the other workloads using the bundled database.

Install only one operator per Kubernetes cluster. When a platform-managed operator already watches the Forma
namespace, keep `authzed.operator.install=false`; the Forma release still owns its `SpiceDBCluster`.
Kubernetes does not upgrade CRDs during a normal Helm upgrade. When changing the bundled operator version, apply
the matching `charts/spicedb-operator/crds/authzed.com_spicedbclusters.yaml` before upgrading the release.

For managed PostgreSQL, create a dedicated database and login outside Helm and expose these keys in a Kubernetes
Secret:

```yaml
stringData:
  datastore_uri: postgresql://spicedb:<password>@postgres.example:5432/spicedb?sslmode=require
  preshared_key: <strong-random-token>
```

Alternatively, the chart can create the dedicated database and login with a short-lived bootstrap Job. Put a
PostgreSQL administrator URL in a separate Secret and enable the external bootstrap:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: forma-postgresql-admin
stringData:
  DATABASE_URL: postgresql://postgres:<admin-password>@postgres.example:5432/postgres?sslmode=require
```

```yaml
authzed:
  externalPostgresqlBootstrap:
    enabled: true
    adminSecretName: forma-postgresql-admin
```

The administrator URL must explicitly set `sslmode=require`, `sslmode=verify-ca`, or `sslmode=verify-full`.
The bootstrap Job validates the TLS mode without logging the URL and passes the URL through unchanged, so other
connection parameters and certificate settings are preserved.

### Bootstrapping against an existing PostgreSQL without a `postgres` role

The bundled bootstrap connects as the `postgres` superuser the subchart normally creates. An existing
installation deployed with `postgresql.auth.enablePostgresUser=false` has no such role, so point the Job at a
role that does hold `CREATEROLE` and `CREATEDB` instead:

```yaml
authzed:
  bundledPostgresqlBootstrap:
    adminUsername: fbadmin
    adminDatabase: forma # the maintenance database to attach to
    adminPasswordSecretName: existing-pg-admin
    adminPasswordKey: password
```

`adminPasswordSecretName` is required whenever `adminUsername` is not the bundled superuser, and
`adminPasswordKey` whenever that Secret is configured explicitly. **Both are enforced when the chart renders**,
so a missing one fails `helm template`/`upgrade` with a named value rather than producing a Job. That is the
point of the guards: without them the first would silently fall back to the bundled admin password and the
second would look up the subchart's key name inside your own Secret — neither of which surfaces until the Pod
is created in the cluster.

`CREATEROLE` and `CREATEDB` cover the normal case, in which this administrator also creates the `spicedb` role.
`CREATE DATABASE ... OWNER spicedb` additionally requires being able to `SET ROLE` to that owner, so the Job
grants itself the `spicedb` role first; from PostgreSQL 16 that is only possible for a role it holds
`ADMIN OPTION` on, which creating the role confers. A `spicedb` role that already exists and was created by
someone else is therefore the one case the Job cannot adopt on PostgreSQL 16+ — grant it explicitly
(`GRANT spicedb TO fbadmin WITH ADMIN OPTION`) or run the bootstrap once as a superuser. PostgreSQL 15 and
older are unaffected.

Re-running is safe but not inert: the role and the database are created only when absent, while the `spicedb`
role's password is reconciled to the chart's Secret on **every** run. If you rotate that password outside Helm,
update the Secret too, or the next upgrade will set it back.
`authzed.bundledPostgresqlBootstrap.enabled=false` remains available for operators who provision both by hand.

Then reference it from the release:

```yaml
authzed:
  enabled: true
  mode: selfHosted
  auth:
    existingSecret: forma-authzed
  datastore:
    existingSecret: forma-authzed
```

To connect to an AuthZed-managed or otherwise external endpoint, use TLS, provide the endpoint without a URL
scheme, and reference a Secret containing `preshared_key`:

```yaml
authzed:
  enabled: true
  mode: external
  operator:
    install: false
  endpoint: grpc.authzed.com:443
  insecure: false
  auth:
    existingSecret: forma-authzed
```

`authzed.insecure` defaults to `false` in external mode. Set it to `true` only for a trusted plaintext gRPC
endpoint; plaintext transport sends the preshared token without TLS protection. The chart injects `AUTHZED_ENABLED`,
`AUTHZED_ENDPOINT`, `AUTHZED_TOKEN`, `AUTHZED_SYSTEM_KEY`, `AUTHZED_INSECURE`, and `AUTHZED_CONSISTENCY` into
the Forma app. Authorization checks must fail closed once product enforcement is enabled; general
Forma readiness remains independent from transient SpiceDB availability.

Fresh installs run a release-matched post-install initialization Job that applies the canonical schema and verifies
the empty or reconciled graph. An acknowledged existing release runs the same release-matched gate as a pre-upgrade
hook; unacknowledged upgrades are rejected before rendering. Before the first v6 upgrade, run:

```bash
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed health
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed schema check
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed upgrade prepare
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed upgrade check

# Empty instances only
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed schema apply

# Non-empty instances: use the remoteDigest returned by the immediately preceding check
kubectl exec -n <namespace> deployment/<release-name> -- forma-authzed schema apply \
  --expected-current-digest sha256:<digest-from-check>
```

The initial apply to an empty instance needs no digest. A non-empty instance must first be checked and then
prepared with `--expected-current-digest sha256:<digest-from-check>`. Once `upgrade check` exits `0`, set
`authzed.migrationAcknowledged=true` in the v6 upgrade values. The chart refuses an unacknowledged upgrade,
`authzed.enabled=false`, or consistency other than `fully_consistent`. Back up the current schema and affected
relationships before replacement; see the repository `authzed/README.md` for exit codes and rollback rules.
The public [AuthZed operations guide](../../docs/self-hosting/advanced/authzed-operations.mdx) covers backups,
restoration, schema lifecycle, relationship repair, and monitoring.

Cloud operators that run the same guarded schema, outbox drain, reconciliation, and audit sequence outside Helm
may set `authzed.initialization.enabled=false` together with `authzed.migrationAcknowledged=true`. This suppresses
the initialization hook so a GitOps sync cannot mutate the authorization graph outside the controlled cutover
window. The acknowledgement must be set only after the external preparation succeeds. Fresh self-hosted installs
should keep the default initialization Job enabled.

## Web AI with self-hosted Qwen/vLLM

The chart can optionally deploy a Forma-provided Qwen runtime through the `vllm-stack` dependency. It is disabled by default so existing installs keep using their current AI provider settings.

To deploy the bundled Qwen/vLLM runtime and automatically point the Forma app at it:

```yaml
llm:
  enabled: true
```

This renders the vLLM router and Qwen serving engine, then injects these app env vars unless you override them in `deployment.env`:

```yaml
AI_PROVIDER: openai-compatible
AI_MODEL: qwen3-14b-awq
AI_OPENAI_COMPATIBLE_BASE_URL: http://<release-name>-router-service:8000/v1
AI_OPENAI_COMPATIBLE_PROVIDER_NAME: vllm
AI_OPENAI_COMPATIBLE_SUPPORTS_STRUCTURED_OUTPUTS: "1"
```

Set `llm.autoConfigureApp=false` to deploy the bundled runtime without injecting Forma app AI env vars.

If you manage your own LLM runtime, keep `llm.enabled=false` and point the web app at your OpenAI-compatible `/v1` endpoint through `deployment.env`.

Only set these variables when you use `AI_PROVIDER=openai-compatible`; Google Vertex, AWS Bedrock, and Azure continue to use their own provider-specific variables.

```yaml
deployment:
  env:
    AI_PROVIDER: openai-compatible
    AI_MODEL: qwen3-14b-awq
    AI_OPENAI_COMPATIBLE_BASE_URL: http://vllm:8000/v1
    AI_OPENAI_COMPATIBLE_PROVIDER_NAME: vllm
    AI_OPENAI_COMPATIBLE_SUPPORTS_STRUCTURED_OUTPUTS: "1"
    AI_OPENAI_COMPATIBLE_API_KEY:
      valueFrom:
        secretKeyRef:
          name: forma-ai-secrets
          key: AI_OPENAI_COMPATIBLE_API_KEY
```

Optional JSON fields such as `AI_OPENAI_COMPATIBLE_HEADERS_JSON` and `AI_OPENAI_COMPATIBLE_QUERY_PARAMS_JSON` can use the same `valueFrom.secretKeyRef` pattern. If you use External Secrets, render a dedicated Secret and reference it from `deployment.env`:

```yaml
externalSecret:
  enabled: true
  files:
    ai-secrets:
      data:
        AI_OPENAI_COMPATIBLE_API_KEY:
          remoteRef:
            key: forma/qwen-vllm
            property: apiKey
```

## Values

| Key                                                                | Type   | Default                                                                     | Description                                               |
| ------------------------------------------------------------------ | ------ | --------------------------------------------------------------------------- | --------------------------------------------------------- |
| autoscaling.additionalLabels                                       | object | `{}`                                                                        |                                                           |
| autoscaling.annotations                                            | object | `{}`                                                                        |                                                           |
| autoscaling.behavior.scaleDown.policies[0].periodSeconds           | int    | `120`                                                                       |                                                           |
| autoscaling.behavior.scaleDown.policies[0].type                    | string | `"Pods"`                                                                    |                                                           |
| autoscaling.behavior.scaleDown.policies[0].value                   | int    | `1`                                                                         |                                                           |
| autoscaling.behavior.scaleDown.stabilizationWindowSeconds          | int    | `300`                                                                       |                                                           |
| autoscaling.behavior.scaleUp.policies[0].periodSeconds             | int    | `60`                                                                        |                                                           |
| autoscaling.behavior.scaleUp.policies[0].type                      | string | `"Pods"`                                                                    |                                                           |
| autoscaling.behavior.scaleUp.policies[0].value                     | int    | `2`                                                                         |                                                           |
| autoscaling.behavior.scaleUp.stabilizationWindowSeconds            | int    | `60`                                                                        |                                                           |
| autoscaling.enabled                                                | bool   | `true`                                                                      |                                                           |
| autoscaling.maxReplicas                                            | int    | `10`                                                                        |                                                           |
| autoscaling.metrics[0].resource.name                               | string | `"cpu"`                                                                     |                                                           |
| autoscaling.metrics[0].resource.target.averageUtilization          | int    | `60`                                                                        |                                                           |
| autoscaling.metrics[0].resource.target.type                        | string | `"Utilization"`                                                             |                                                           |
| autoscaling.metrics[0].type                                        | string | `"Resource"`                                                                |                                                           |
| autoscaling.metrics[1].resource.name                               | string | `"memory"`                                                                  |                                                           |
| autoscaling.metrics[1].resource.target.averageUtilization          | int    | `60`                                                                        |                                                           |
| autoscaling.metrics[1].resource.target.type                        | string | `"Utilization"`                                                             |                                                           |
| autoscaling.metrics[1].type                                        | string | `"Resource"`                                                                |                                                           |
| autoscaling.minReplicas                                            | int    | `1`                                                                         |                                                           |
| componentOverride                                                  | string | `""`                                                                        |                                                           |
| deployment.additionalLabels                                        | object | `{}`                                                                        |                                                           |
| deployment.additionalPodAnnotations                                | object | `{}`                                                                        |                                                           |
| deployment.additionalPodLabels                                     | object | `{}`                                                                        |                                                           |
| deployment.affinity                                                | object | `{}`                                                                        |                                                           |
| deployment.annotations                                             | object | `{}`                                                                        |                                                           |
| deployment.args                                                    | list   | `[]`                                                                        |                                                           |
| deployment.command                                                 | list   | `[]`                                                                        |                                                           |
| deployment.containerSecurityContext.readOnlyRootFilesystem         | bool   | `true`                                                                      |                                                           |
| deployment.containerSecurityContext.runAsNonRoot                   | bool   | `true`                                                                      |                                                           |
| deployment.env                                                     | object | `{}`                                                                        | App container environment variables. Supports scalar values and `valueFrom` maps such as `secretKeyRef`. |
| deployment.envFrom                                                 | string | `nil`                                                                       | Additional app container environment sources from ConfigMaps or Secrets. |
| deployment.extraVolumeMounts                                       | list   | `[]`                                                                        | Additional app container volume mounts.                   |
| deployment.extraVolumes                                            | list   | `[]`                                                                        | Additional app pod volumes.                               |
| deployment.image.digest                                            | string | `""`                                                                        | When set, takes precedence over tag.                      |
| deployment.image.pullPolicy                                        | string | `"IfNotPresent"`                                                            |                                                           |
| deployment.image.repository                                        | string | `"ghcr.io/yldm-tech/forma"`                                           |                                                           |
| deployment.image.tag                                               | string | `""`                                                                        |                                                           |
| deployment.imagePullSecrets                                        | string | `""`                                                                        |                                                           |
| deployment.lifecycle                                               | object | `{}`                                                                        | Optional app container lifecycle hooks.                   |
| deployment.nodeSelector                                            | object | `{}`                                                                        |                                                           |
| deployment.ports.http.containerPort                                | int    | `3000`                                                                      |                                                           |
| deployment.ports.http.exposed                                      | bool   | `true`                                                                      |                                                           |
| deployment.ports.http.protocol                                     | string | `"TCP"`                                                                     |                                                           |
| deployment.ports.metrics.containerPort                             | int    | `9464`                                                                      |                                                           |
| deployment.ports.metrics.exposed                                   | bool   | `true`                                                                      |                                                           |
| deployment.ports.metrics.protocol                                  | string | `"TCP"`                                                                     |                                                           |
| deployment.probes.livenessProbe.failureThreshold                   | int    | `5`                                                                         |                                                           |
| deployment.probes.livenessProbe.httpGet.path                       | string | `"/health"`                                                                 |                                                           |
| deployment.probes.livenessProbe.httpGet.port                       | int    | `3000`                                                                      |                                                           |
| deployment.probes.livenessProbe.initialDelaySeconds                | int    | `10`                                                                        |                                                           |
| deployment.probes.livenessProbe.periodSeconds                      | int    | `10`                                                                        |                                                           |
| deployment.probes.livenessProbe.successThreshold                   | int    | `1`                                                                         |                                                           |
| deployment.probes.livenessProbe.timeoutSeconds                     | int    | `5`                                                                         |                                                           |
| deployment.probes.readinessProbe.failureThreshold                  | int    | `5`                                                                         |                                                           |
| deployment.probes.readinessProbe.httpGet.path                      | string | `"/health"`                                                                 |                                                           |
| deployment.probes.readinessProbe.httpGet.port                      | int    | `3000`                                                                      |                                                           |
| deployment.probes.readinessProbe.initialDelaySeconds               | int    | `10`                                                                        |                                                           |
| deployment.probes.readinessProbe.periodSeconds                     | int    | `10`                                                                        |                                                           |
| deployment.probes.readinessProbe.successThreshold                  | int    | `1`                                                                         |                                                           |
| deployment.probes.readinessProbe.timeoutSeconds                    | int    | `5`                                                                         |                                                           |
| deployment.probes.startupProbe.failureThreshold                    | int    | `30`                                                                        |                                                           |
| deployment.probes.startupProbe.periodSeconds                       | int    | `10`                                                                        |                                                           |
| deployment.probes.startupProbe.tcpSocket.port                      | int    | `3000`                                                                      |                                                           |
| deployment.reloadOnChange                                          | bool   | `false`                                                                     |                                                           |
| deployment.replicas                                                | int    | `1`                                                                         |                                                           |
| deployment.resources.limits.memory                                 | string | `"2Gi"`                                                                     |                                                           |
| deployment.resources.requests.cpu                                  | string | `"1"`                                                                       |                                                           |
| deployment.resources.requests.memory                               | string | `"1Gi"`                                                                     |                                                           |
| deployment.revisionHistoryLimit                                    | int    | `2`                                                                         |                                                           |
| deployment.securityContext                                         | object | `{}`                                                                        |                                                           |
| deployment.strategy.type                                           | string | `"RollingUpdate"`                                                           |                                                           |
| deployment.terminationGracePeriodSeconds                           | int    | `30`                                                                        | Time allowed for graceful Pod shutdown; must exceed any preStop drain. |
| deployment.tolerations                                             | list   | `[]`                                                                        |                                                           |
| deployment.topologySpreadConstraints                               | list   | `[]`                                                                        |                                                           |
| enterprise.enabled                                                 | bool   | `false`                                                                     | Deprecated compatibility value; it has no template effect. |
| enterprise.licenseKey                                              | string | `""`                                                                        | Adds the license to the chart-generated app Secret.       |
| externalSecret.enabled                                             | bool   | `false`                                                                     |                                                           |
| externalSecret.files                                               | object | `{}`                                                                        |                                                           |
| externalSecret.refreshInterval                                     | string | `"1h"`                                                                      |                                                           |
| externalSecret.secretStore.kind                                    | string | `"ClusterSecretStore"`                                                      |                                                           |
| externalSecret.secretStore.name                                    | string | `"aws-secrets-manager"`                                                     |                                                           |
| forma.mcpOauthJwksUrl                                         | string | `""`                                                                        | Optional internal JWKS fetch URL for trusted application networks. |
| forma.publicUrl                                               | string | `""`                                                                        |                                                           |
| forma.webappUrl                                               | string | `""`                                                                        |                                                           |
| ingress.annotations                                                | object | `{}`                                                                        |                                                           |
| ingress.enabled                                                    | bool   | `false`                                                                     |                                                           |
| ingress.hosts[0].host                                              | string | `"k8s.forma.ylam.ai"`                                                      |                                                           |
| ingress.hosts[0].paths[0].path                                     | string | `"/"`                                                                       |                                                           |
| ingress.hosts[0].paths[0].pathType                                 | string | `"Prefix"`                                                                  |                                                           |
| ingress.hosts[0].paths[0].serviceName                              | string | `"forma"`                                                              |                                                           |
| ingress.ingressClassName                                           | string | `"alb"`                                                                     |                                                           |
| llm.autoConfigureApp                                               | bool   | `true`                                                                      | Inject OpenAI-compatible app env vars when bundled Qwen/vLLM is enabled. |
| llm.enabled                                                        | bool   | `false`                                                                     | Deploy bundled Qwen/vLLM through the optional vllm-stack dependency. |
| llm.forma.baseUrl                                             | string | `""`                                                                        | Defaults to `http://<release-name>-router-service:<llm.routerSpec.servicePort>/v1`. |
| llm.forma.model                                               | string | `"qwen3-14b-awq"`                                                           | Forma `AI_MODEL` value for the bundled runtime.      |
| llm.forma.providerName                                        | string | `"vllm"`                                                                    | Forma OpenAI-compatible provider display name.       |
| llm.forma.supportsStructuredOutputs                           | string | `"1"`                                                                       | Enables structured output usage for the bundled runtime.  |
| llm.routerSpec.enableRouter                                        | bool   | `true`                                                                      | Enable the vLLM router service.                           |
| llm.routerSpec.k8sServiceDiscoveryType                             | string | `"service-name"`                                                            | vLLM router Kubernetes service discovery mode.            |
| llm.routerSpec.servicePort                                         | int    | `8000`                                                                      | vLLM router service port used by the app base URL.        |
| llm.routerSpec.serviceType                                         | string | `"ClusterIP"`                                                               | vLLM router service type.                                 |
| llm.servingEngineSpec.enableEngine                                 | bool   | `true`                                                                      | Enable the vLLM serving engine.                           |
| llm.servingEngineSpec.modelSpec[0].modelURL                        | string | `"Qwen/Qwen3-14B-AWQ"`                                                      | Hugging Face model loaded by vLLM.                        |
| llm.servingEngineSpec.modelSpec[0].name                            | string | `"qwen"`                                                                    | vLLM model spec name.                                     |
| llm.servingEngineSpec.modelSpec[0].repository                      | string | `"vllm/vllm-openai"`                                                        | vLLM runtime image repository.                            |
| llm.servingEngineSpec.modelSpec[0].requestGPU                      | int    | `1`                                                                         | GPU request for the Qwen serving pod.                     |
| llm.servingEngineSpec.modelSpec[0].requestGPUType                  | string | `"nvidia.com/gpu"`                                                          | Kubernetes GPU resource key.                              |
| llm.servingEngineSpec.modelSpec[0].tag                             | string | `"v0.14.0"`                                                                 | vLLM runtime image tag.                                   |
| llm.servingEngineSpec.servicePort                                  | int    | `8000`                                                                      | Qwen serving engine service port.                         |
| llm.servingEngineSpec.strategy.type                                | string | `"Recreate"`                                                                | Avoids requiring a second GPU during model pod upgrades.  |
| migration.annotations                                              | object | `{}`                                                                        |                                                           |
| migration.backoffLimit                                             | int    | `3`                                                                         |                                                           |
| migration.enabled                                                  | bool   | `true`                                                                      |                                                           |
| migration.resources.limits.memory                                  | string | `"512Mi"`                                                                   |                                                           |
| migration.resources.requests.cpu                                   | string | `"100m"`                                                                    |                                                           |
| migration.resources.requests.memory                                | string | `"256Mi"`                                                                   |                                                           |
| migration.ttlSecondsAfterFinished                                  | int    | `300`                                                                       |                                                           |
| migration.waitForDatabase.connectionTimeoutSeconds                 | int    | `5`                                                                         | Per-attempt TCP connection timeout.                       |
| migration.waitForDatabase.enabled                                  | bool   | `true`                                                                      | Wait for PostgreSQL before starting migrations.           |
| migration.waitForDatabase.intervalSeconds                          | int    | `5`                                                                         | Delay between readiness attempts.                         |
| migration.waitForDatabase.timeoutSeconds                           | int    | `900`                                                                       | Overall readiness timeout for each Job attempt.           |
| nameOverride                                                       | string | `""`                                                                        |                                                           |
| partOfOverride                                                     | string | `""`                                                                        |                                                           |
| pdb.additionalLabels                                               | object | `{}`                                                                        |                                                           |
| pdb.annotations                                                    | object | `{}`                                                                        |                                                           |
| pdb.enabled                                                        | bool   | `true`                                                                      |                                                           |
| pdb.minAvailable                                                   | int    | `1`                                                                         |                                                           |
| postgresql.auth.database                                           | string | `"forma"`                                                              |                                                           |
| postgresql.auth.enablePostgresUser                                 | bool   | `true`                                                                       | Required by the bundled AuthZed database bootstrap.       |
| postgresql.auth.existingSecret                                     | string | `"forma-app-secrets"`                                                  |                                                           |
| postgresql.auth.secretKeys.adminPasswordKey                        | string | `"POSTGRES_ADMIN_PASSWORD"`                                                 |                                                           |
| postgresql.auth.secretKeys.userPasswordKey                         | string | `"POSTGRES_USER_PASSWORD"`                                                  |                                                           |
| postgresql.auth.username                                           | string | `"forma"`                                                              |                                                           |
| postgresql.commonAnnotations                                       | object | `{"argocd.argoproj.io/sync-wave":"-2"}`                                   | Order bundled PostgreSQL before migration hooks in Argo.  |
| postgresql.enabled                                                 | bool   | `true`                                                                      |                                                           |
| postgresql.externalDatabaseUrl                                     | string | `""`                                                                        |                                                           |
| postgresql.fullnameOverride                                        | string | `"forma-postgresql"`                                                   |                                                           |
| postgresql.global.security.allowInsecureImages                     | bool   | `true`                                                                      |                                                           |
| postgresql.image.repository                                        | string | `"pgvector/pgvector"`                                                       |                                                           |
| postgresql.image.tag                                               | string | `"pg17"`                                                                    |                                                           |
| postgresql.primary.containerSecurityContext.enabled                | bool   | `true`                                                                      |                                                           |
| postgresql.primary.containerSecurityContext.readOnlyRootFilesystem | bool   | `false`                                                                     |                                                           |
| postgresql.primary.containerSecurityContext.runAsUser              | int    | `1001`                                                                      |                                                           |
| postgresql.primary.networkPolicy.enabled                           | bool   | `false`                                                                     |                                                           |
| postgresql.primary.persistence.enabled                             | bool   | `true`                                                                      |                                                           |
| postgresql.primary.persistence.size                                | string | `"10Gi"`                                                                    |                                                           |
| postgresql.primary.podSecurityContext.enabled                      | bool   | `true`                                                                      |                                                           |
| postgresql.primary.podSecurityContext.fsGroup                      | int    | `1001`                                                                      |                                                           |
| postgresql.primary.podSecurityContext.runAsUser                    | int    | `1001`                                                                      |                                                           |
| postgresql.primary.resources.limits.cpu                            | string | `"1"`                                                                       |                                                           |
| postgresql.primary.resources.limits.memory                         | string | `"1Gi"`                                                                     |                                                           |
| postgresql.primary.resources.requests.cpu                          | string | `"250m"`                                                                    |                                                           |
| postgresql.primary.resources.requests.memory                       | string | `"512Mi"`                                                                   |                                                           |
| rbac.enabled                                                       | bool   | `false`                                                                     |                                                           |
| rbac.serviceAccount.additionalLabels                               | object | `{}`                                                                        |                                                           |
| rbac.serviceAccount.annotations                                    | object | `{}`                                                                        |                                                           |
| rbac.serviceAccount.enabled                                        | bool   | `false`                                                                     |                                                           |
| rbac.serviceAccount.name                                           | string | `""`                                                                        |                                                           |
| redis.architecture                                                 | string | `"standalone"`                                                              |                                                           |
| redis.auth.enabled                                                 | bool   | `true`                                                                      |                                                           |
| redis.auth.existingSecret                                          | string | `"forma-app-secrets"`                                                  |                                                           |
| redis.auth.existingSecretPasswordKey                               | string | `"REDIS_PASSWORD"`                                                          |                                                           |
| redis.enabled                                                      | bool   | `true`                                                                      |                                                           |
| redis.externalRedisUrl                                             | string | `""`                                                                        |                                                           |
| redis.fullnameOverride                                             | string | `"forma-redis"`                                                        |                                                           |
| redis.image.digest                                                 | string | `"sha256:e0eb7c480958d32bdc4357a74bdd70653ae15f2f9b4c93c4a5a9fad1dc471c84"` |                                                           |
| redis.image.pullPolicy                                             | string | `"IfNotPresent"`                                                            |                                                           |
| redis.image.repository                                             | string | `"valkey/valkey"`                                                           |                                                           |
| redis.image.tag                                                    | string | `""`                                                                        |                                                           |
| redis.master.affinity                                              | object | `{}`                                                                        |                                                           |
| redis.master.containerSecurityContext                              | object | `{}`                                                                        |                                                           |
| redis.master.nodeSelector                                          | object | `{}`                                                                        |                                                           |
| redis.master.pdb.enabled                                           | bool   | `true`                                                                      |                                                           |
| redis.master.pdb.maxUnavailable                                    | int    | `0`                                                                         |                                                           |
| redis.master.pdb.minAvailable                                      | string | `""`                                                                        |                                                           |
| redis.master.persistence.accessModes[0]                            | string | `"ReadWriteOnce"`                                                           |                                                           |
| redis.master.persistence.enabled                                   | bool   | `true`                                                                      |                                                           |
| redis.master.persistence.size                                      | string | `"8Gi"`                                                                     |                                                           |
| redis.master.persistence.storageClass                              | string | `""`                                                                        |                                                           |
| redis.master.podAnnotations                                        | object | `{}`                                                                        |                                                           |
| redis.master.podSecurityContext                                    | object | `{}`                                                                        |                                                           |
| redis.master.resources.limits.cpu                                  | string | `"150m"`                                                                    |                                                           |
| redis.master.resources.limits.memory                               | string | `"192Mi"`                                                                   |                                                           |
| redis.master.resources.requests.cpu                                | string | `"100m"`                                                                    |                                                           |
| redis.master.resources.requests.memory                             | string | `"128Mi"`                                                                   |                                                           |
| redis.master.tolerations                                           | list   | `[]`                                                                        |                                                           |
| redis.master.topologySpreadConstraints                             | list   | `[]`                                                                        |                                                           |
| redis.networkPolicy.enabled                                        | bool   | `false`                                                                     |                                                           |
| secret.enabled                                                     | bool   | `true`                                                                      |                                                           |
| service.additionalLabels                                           | object | `{}`                                                                        |                                                           |
| service.annotations                                                | object | `{}`                                                                        |                                                           |
| service.enabled                                                    | bool   | `true`                                                                      |                                                           |
| service.ports                                                      | list   | `[]`                                                                        |                                                           |
| service.type                                                       | string | `"ClusterIP"`                                                               |                                                           |
| serviceMonitor.additionalLabels                                    | string | `nil`                                                                       |                                                           |
| serviceMonitor.annotations                                         | string | `nil`                                                                       |                                                           |
| serviceMonitor.enabled                                             | bool   | `true`                                                                      |                                                           |
| serviceMonitor.endpoints[0].interval                               | string | `"5s"`                                                                      |                                                           |
| serviceMonitor.endpoints[0].path                                   | string | `"/metrics"`                                                                |                                                           |
| serviceMonitor.endpoints[0].port                                   | string | `"metrics"`                                                                 |                                                           |
