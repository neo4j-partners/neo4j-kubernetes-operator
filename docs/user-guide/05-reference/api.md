# API reference — Neo4j v1beta1

Every field of the `Neo4j` custom resource, with its type, default and constraints. For explanations
and worked examples, use the [topic pages](../03-neo4j/readme.md); this page is for looking things up.

| Property | Value |
|----------|-------|
| API group | `neo4j.com` |
| Version | `v1beta1` |
| Kind | `Neo4j` |
| Short name | `n4j` |
| Scope | Namespaced |

```bash
kubectl get neo4j
kubectl get n4j -o wide
kubectl explain neo4j.spec.topology
```

`kubectl get` prints edition, version, mode, readiness and age. The read pool is wired to the scale
subresource, so `kubectl scale neo4j <name> --replicas=N` adjusts
`spec.topology.secondaries.read.members`.

## Minimal resource

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
```

Required: `edition`, `version`, `topology.mode`, plus `license.accept` on `enterprise`. In practice
`storage.volumes.data` is required too, since a database with no data volume is not useful.

## Identity

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.edition` | string | — | **Required.** `enterprise` or `community`. Community is restricted to `topology.mode: Standalone` and cannot use `features.backup` or `features.monitoring.prometheus` |
| `spec.version` | string | — | **Required.** Neo4j calendar version, for example `2026.05.0`; the `-enterprise` image suffix is added for you, and community uses the unsuffixed tag. Changing it later is not orchestrated |
| `spec.license.accept` | string | — | `yes` or `eval`. **Required on `enterprise`**; on `community` the whole `license` block may be omitted, and is ignored if present |
| `spec.image.repository` | string | `neo4j` | Must match operator allowlist (NEO-012); default `neo4j` / `docker.io/neo4j` |
| `spec.image.digest` | string | — | Optional `sha256:…` pin; renders `repo@digest` instead of `:tag` |
| `spec.image.pullPolicy` | string | `IfNotPresent` | `Always`, `IfNotPresent` or `Never` |
| `spec.image.pullSecrets` | []string | — | Names of image pull Secrets in the workload namespace |

## Topology

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.topology.mode` | string | — | **Required.** `Standalone` or `Cluster`. **Immutable** |
| `spec.topology.primaries.members` | int32 | — | Required in Cluster mode, minimum 1, maximum 15, must be **odd** |
| `spec.topology.primaries.plugins` | []string | — | Max 8; `gds` and `bloom` are rejected here in Cluster mode |
| `spec.topology.secondaries.analytics.members` | int32 | — | Minimum 1 when the pool is declared; maximum 25 |
| `spec.topology.secondaries.analytics.plugins` | []string | — | The pool for `gds` and `bloom` |
| `spec.topology.secondaries.read.members` | int32 | — | Minimum 1 when the pool is declared; maximum 25; target of `kubectl scale` |
| `spec.topology.secondaries.read.plugins` | []string | — | `gds` and `bloom` are rejected here |
| `spec.topology.defaultPrimariesCount` | int32 | `1` | Primaries given to a standard database created without an explicit topology. Existing databases are not rewritten to match. Minimum 1, maximum 15, cannot exceed `primaries.members` |
| `spec.topology.minimumMembers` | int32 | `1` with one primary, `3` otherwise | Primaries that must meet before the `system` database bootstraps. Minimum 1, maximum 15; odd and even values both accepted; `1` only on a single-primary cluster. At creation it cannot exceed `primaries.members`. **Immutable** — a later scale-in may leave it above the pool, which is harmless |

The derived default ignores the pool size on purpose, so scaling never rewrites `neo4j.conf`. Raising
the gate is the only reason to set the field. See
[Clustering](../03-neo4j/02-clustering.md#the-system-bootstrap-gate).

Standalone mode rejects `primaries`, `secondaries`, `minimumMembers` and `defaultPrimariesCount`.
Cluster mode requires `primaries.members`, and `secondaries` requires `primaries.members` to be set
first.

See [Clustering](../03-neo4j/02-clustering.md).

## Storage

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.storage.volumes.data.mode` | string | — | **Required** within `data`: `Dynamic` or `Existing`. `Share` is rejected |
| `spec.storage.volumes.data.dynamic.size` | quantity | — | Required when mode is `Dynamic`; maximum 16Ti (NEO-014) |
| `spec.storage.volumes.data.dynamic.storageClassName` | string | cluster default | |
| `spec.storage.volumes.data.dynamic.accessMode` | string | `ReadWriteOnce` | Only value accepted |
| `spec.storage.volumes.data.dynamic.labels` | map | — | Extra labels on the generated claims |
| `spec.storage.volumes.data.existing.claimName` | string | — | An existing PersistentVolumeClaim; never deleted by the operator |
| `spec.storage.volumes.data.existing.volumeClaimTemplate` | PVC spec | — | One claim provisioned per pod |
| `spec.storage.volumes.data.existing.volume` | Volume | — | Any inline Kubernetes volume |
| `spec.storage.volumes.data.disableSubPathExpr` | bool | `false` | Opt out of per-pod subpaths |
| `spec.storage.volumes.{backups,logs,metrics,import,licenses,plugins}` | object | — | Auxiliary volumes, each with `mode` (`Share`, `Dynamic`, `Existing`), `shareFrom: data`, `dynamic`, `existing` |
| `spec.storage.volumeClaimRetention.whenDeleted` | string | `Retain` | `Retain` or `Delete`; pinned at creation into status |
| `spec.storage.volumeClaimRetention.whenScaled` | string | `Retain` | `Retain` or `Delete` |
| `spec.storage.additionalMounts[]` | list | — | `name`, `volume`, `mountPath`, optional `subPath`, `readOnly` |
| `spec.storage.secretMounts.<key>` | object | — | `secretName`, `mountPath`, `items[].key`/`.path`, optional `defaultMode` |

Exactly one source may be set under `existing`. Reserved mount paths — `/data`, `/backups`,
`/config`, `/plugins`, `/logs`, `/metrics`, `/import`, `/licenses` — are refused for additional and
secret mounts, and Secrets must carry the mountable label with explicit `items`.

See [Storage](../03-neo4j/03-storage.md).

## Authentication

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.auth.generatePassword` | bool | — | Creates `<name>-auth` with a random password |
| `spec.auth.passwordSecretRef.name` | string | — | Existing Secret with key `NEO4J_AUTH` as `user/password` |
| `spec.auth.ldap.enabled` | bool | `false` | Ignored — approach not decided |
| `spec.auth.ldap.passwordSecretRef.name` | string | — | Ignored — approach not decided |

`generatePassword: true` and `passwordSecretRef` are mutually exclusive. A referenced Secret needs
both `neo4j.com/mountable-by-operator=true` and `neo4j.com/allowed-for=<resource name>`.

See [Security](../03-neo4j/05-security.md#authentication).

## Configuration

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.config.neo4j` | map[string]string | — | `neo4j.conf` keys; some are refused or overridden |
| `spec.config.apoc` | map[string]string | — | `apoc.conf` keys |
| `spec.config.jvm.useDefaults` | bool | `true` | Include Neo4j's default JVM arguments |
| `spec.config.jvm.additionalArguments` | []string | — | Max 64; appended after the defaults, replacing any with the same key |
| `spec.logging.serverLogsXml` | string | — | Full `server-logs.xml` |
| `spec.logging.serverLogsConfigMapRef` | object | — | `name`, optional `key`; alternative to the inline XML |
| `spec.logging.userLogsXml` | string | — | Full `user-logs.xml` |
| `spec.logging.userLogsConfigMapRef` | object | — | `name`, optional `key` |

Per logging side, inline XML and a ConfigMap reference are mutually exclusive. Refused and
operator-owned `neo4j.conf` keys are listed in [Operator-owned settings](operator-owned-config.md).

See [Configuration](../03-neo4j/06-configuration.md).

## Plugins

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.plugins` | []string | — | Max 8, from `apoc`, `gds`, `bloom`. **Standalone only** — rejected in Cluster mode |
| `spec.pluginDefinitions.<id>.licenseSecretRef` | string | — | Secret mounted at `/licenses/<id>`; needs the mountable label |
| `spec.pluginDefinitions.<id>.version` | string | — | Rejected — pin JARs via Existing `/plugins` volume or a custom image (NEO-013) |
| `spec.pluginDefinitions.<id>.config` | map[string]string | — | Merged into `neo4j.conf` below your own configuration |

In Cluster mode plugins are declared per pool under `spec.topology`.

See [Plugins](../03-neo4j/07-plugins.md).

## Connectivity

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.connectivity.listeners.bolt` | int32 | `7687` | Always enabled |
| `spec.connectivity.listeners.http` | int32 | `7474` | Always enabled |
| `spec.connectivity.listeners.https` | int32 | `7473` when set | Setting the field enables the listener |
| `spec.connectivity.listeners.backup` | int32 | `6362` when set | Requires `features.backup.enabled: true` |
| `spec.connectivity.listeners.metrics` | int32 | `2004` when set | Requires `features.monitoring.prometheus.enabled: true` |
| `spec.connectivity.service.type` | string | `ClusterIP` | `ClusterIP`, `NodePort` or `LoadBalancer` |
| `spec.connectivity.service.expose` | []string | `[bolt, http]` | Filter over enabled connectors |
| `spec.connectivity.service.ports.{bolt,http,https,backup,metrics}` | int32 | listener port | Remap the Service port (1–65535) |
| `spec.connectivity.service.annotations` | map | — | Copied onto the Service |
| `spec.connectivity.service.loadBalancerSourceRanges` | []string | — | **Required** when type is `LoadBalancer` |
| `spec.connectivity.clusterDomain` | string | `cluster.local` | Suffix for Neo4j-advertised names |
| `spec.connectivity.ingress.*` | object | — | Ignored — planned |
| `spec.connectivity.reverseProxy.*` | object | — | Ignored — planned |
| `spec.connectivity.multiCluster.enabled` | bool | `false` | `true` is rejected |

See [Connectivity](../03-neo4j/04-connectivity.md).

## Trust and TLS

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.trust.enabled` | bool | `false` | |
| `spec.trust.insecureAdminConnection` | bool | `false` | Lets the operator open its admin Bolt session in cleartext. **Cluster mode requires either this or `trust.certificates.bolt` with `trust.enabled: true`**, and a Cluster with neither is rejected at admission |
| `spec.trust.certificates.{bolt,https,cluster}.privateKey` | object | — | `secretName`, optional `subPath` |
| `spec.trust.certificates.{bolt,https,cluster}.publicCertificate` | object | — | `secretName`, optional `subPath` |
| `spec.trust.certificates.*.secretName` | string | — | cert-manager target Secret (`tls.crt` + `tls.key`); mutually exclusive with `privateKey`/`publicCertificate` |
| `spec.trust.certificates.*.dnsNames` | []string | — | Extra SANs on that policy's cert-manager Certificate |
| `spec.trust.certificates.*.clientAuth` | string | `None` | `None`, `Optional` or `Require`; `Optional` and `Require` both require `trustedCerts.sources` |
| `spec.trust.certificates.*.trustedCerts.sources` | []VolumeProjection | — | CA bundles; items must be named |
| `spec.trust.reload.enabled` | bool | `false` | Turns on Neo4j TLS reload. Leaf key/cert are `subPath` mounts; the operator rolls pods when those Secret bytes change — see [Security](../03-neo4j/05-security.md#certificate-renewal) |
| `spec.trust.certManager.enabled` | bool | `false` | Operator creates one `Certificate` per policy; requires cert-manager installed and `issuerRef.name` |
| `spec.trust.certManager.issuerRef` | object | — | `name`, `kind` (`Issuer` or `ClusterIssuer`, default `ClusterIssuer`) |
| `spec.trust.certManager.dnsNames` | []string | — | Extra SANs merged into the bolt and https Certificates only |
| `spec.trust.certManager.includeIngressHosts` | bool | `false` | Merge `connectivity.ingress.rules[].host` into bolt/https SANs; requires at least one host |

With `trust.enabled: true`, Cluster mode requires `certificates.cluster`, and Standalone requires at
least one of `certificates.bolt` or `certificates.https` while rejecting `cluster`.

See [Security](../03-neo4j/05-security.md#transport-security).

## Features

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.features.backup.enabled` | bool | `false` | Enables the backup listener and the admin Service; no backup workflow |
| `spec.features.monitoring.prometheus.enabled` | bool | `false` | Prometheus endpoint inside Neo4j |
| `spec.features.monitoring.prometheus.endpoint` | string | — | Ignored; the port comes from the listener |
| `spec.features.monitoring.serviceMonitor.enabled` | bool | `false` | Creates `<name>-servicemonitor` |
| `spec.features.monitoring.serviceMonitor.interval` | string | `30s` | |
| `spec.features.monitoring.serviceMonitor.port` | string | `tcp-prometheus` | |
| `spec.features.monitoring.serviceMonitor.path` | string | `/metrics` | |
| `spec.features.monitoring.serviceMonitor.labels` | map | — | Usually needed for Prometheus to select it |
| `spec.features.monitoring.serviceMonitor.jobLabel`, `.targetLabels`, `.selector` | — | — | Standard ServiceMonitor fields |
| `spec.features.monitoring.serviceMonitor.namespaceSelector` | selector | — | Accepted, not applied |
| `spec.features.monitoring.{csv,jmx,graphite}` | object | — | Ignored — approach not decided |

See [Monitoring](../03-neo4j/08-monitoring.md).

## Workload shaping

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `spec.resources` | ResourceRequirements | — | Requests and limits for the Neo4j container. Unset is allowed; production should set them (or a namespace ResourceQuota) |
| `spec.scheduling.nodeSelector` | map | — | |
| `spec.scheduling.tolerations` | []Toleration | — | |
| `spec.scheduling.affinity.podAntiAffinity` | string | — | `soft`, `hard` or `custom` |
| `spec.scheduling.affinity.custom` | Affinity | — | Used with the `custom` preset |
| `spec.scheduling.topologySpreadConstraints` | []TopologySpreadConstraint | — | |
| `spec.scheduling.priorityClassName` | string | — | |
| `spec.scheduling.terminationGracePeriodSeconds` | int64 | `3600` | Forced to 0 in offline maintenance |
| `spec.podDisruptionBudget.enabled` | bool | `false` | |
| `spec.podDisruptionBudget.minAvailable` | int or string | — | Must be satisfiable; `100%` is rejected |
| `spec.probes.{startup,liveness,readiness}` | Probe | TCP on Bolt | An override replaces the default entirely |
| `spec.security.podSecurityContext` | PodSecurityContext | uid/gid/fsGroup 7474 | |
| `spec.security.containerSecurityContext` | SecurityContext | — | |
| `spec.security.serviceAccount.create` | bool | `false` | ServiceAccount named after the resource |
| `spec.security.serviceAccount.annotations` | map | — | Cloud workload-identity annotations are rejected |
| `spec.security.networkPolicy.enabled` | bool | `false` | Opt-in; requires `ingressFrom` when true (NEO-010) |
| `spec.security.networkPolicy.ingressFrom` | []NetworkPolicyPeer | — | Peers for Bolt/HTTP/HTTPS |
| `spec.security.networkPolicy.backupFrom` | []NetworkPolicyPeer | — | Optional; defaults to `ingressFrom` |
| `spec.security.networkPolicy.metricsFrom` | []NetworkPolicyPeer | — | Optional; defaults to `ingressFrom` |
| `spec.maintenance.offlineMode` | bool | `false` | Runs an idle container instead of Neo4j |

See [Operations](../03-neo4j/09-operations.md).

## Status

Read status rather than inferring state from pods. Conditions are the contract; `phase` is a summary.

| Field | Type | Meaning |
|-------|------|---------|
| `status.phase` | string | `Pending`, `Provisioning`, `Bootstrapping`, `Running`, `Degraded`, `Failed`, `Maintenance` |
| `status.conditions` | []Condition | Standard Kubernetes conditions; see the table below |
| `status.observedGeneration` | int64 | Last `metadata.generation` fully reconciled |
| `status.version` | string | Version observed on the members |
| `status.serverSummary.servers` / `.ready` | int32 | Desired and ready replica counts |
| `status.members[]` | list | Per-server name, pool, address, plugins, Neo4j state and health, hosted databases, pod summary |
| `status.endpoints.{bolt,neo4j,http,https,backup,internal}` | string | Client URIs |
| `status.endpoints.connectionExamples` | object | Ready-to-paste Bolt URI, port-forward command, Python and Java snippets |
| `status.credentials.secretName` / `.generated` | string / bool | Where the password lives — never the password |
| `status.clusterInfo.clusterId` / `.databases[]` | string / list | Cluster identity and database states |
| `status.diagnostics` | object | `SHOW SERVERS` and `SHOW DATABASES` snapshots, with `lastCollectedTime`; failures here do not affect `Ready` |
| `status.readPoolReplicas` | int32 | Observed read pool size, for the scale subresource |
| `status.volumeClaimRetentionWhenDeleted` | string | The retention policy pinned at creation |
| `status.upgrade`, `status.lastUpgradeTime` | object | Reserved for orchestrated upgrades; empty today |

Conditions you will actually gate on:

| Condition | True means |
|-----------|-----------|
| `Ready` | The workload is reachable for the current generation |
| `Installed` | The base Kubernetes objects exist |
| `Reconciling` | A reconcile is in progress |
| `Error` | Reconcile failed; the reason names the cause |
| `StorageReady` | Data claims are bound |
| `TLSReady` | TLS material is present and usable |
| `ClusterFormed` | Members formed the cluster and database allocation matches the spec |
| `ServersPendingDrain` | A scale-in is draining servers |

Every `reason` value, with severity and meaning, is in the [error reference](errors.md).

```bash
kubectl get neo4j dev -n default \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'

kubectl wait --for=condition=Ready neo4j/dev -n default --timeout=10m
```

Operator-owned status fields — `drainOK`, `drainOKGeneration`, `primaryReplicasCap` — exist so scale-in
decisions cannot be forged from outside. Do not write them.

## Immutable and rejected changes

| Change | Result |
|--------|--------|
| `spec.topology.mode` | Rejected — create a new resource instead |
| `spec.topology.minimumMembers` | Rejected after creation — Neo4j reads it at first bootstrap only |
| Cluster primaries from several to exactly 1 | Held with reason `UnsupportedSinglePrimary` |
| Cluster primaries from 1 upwards | Held with reason `UnsupportedSystemScaleUp` |
| `whenDeleted: Delete` patched after creation | Accepted but not armed; the pinned value stands |
| `spec.version` | Accepted; the rollout is not orchestrated |

## Related

[Neo4j topics](../03-neo4j/readme.md) · [Operator-owned settings](operator-owned-config.md) ·
[Error reference](errors.md) · [Examples](../../../examples/)
