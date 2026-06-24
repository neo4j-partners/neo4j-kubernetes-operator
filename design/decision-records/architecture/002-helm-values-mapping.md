# ADR-002 — Helm values → `Neo4j` spec mapping

| | |
|---|---|
| **Status** | accepted |
| **Date** | 2026-06-24 |
| **Reviewers** | Charles Boudry, Marouane Gazanayi |
| **Depends on** | [BDR-002](../business/002-neo4j-crd-topology.md), [BDR-005](../business/005-v1-full-crd-scope.md) |
| **Constraints** | `helm-charts/neo4j/values.yaml`, `analysis/helm_neo4j_values.yaml` |

---

## Context

The operator renders Helm-equivalent Kubernetes objects from a `Neo4j` CR. Users migrating from the official Neo4j Helm chart need a **translation table**, not a CR that mirrors Helm field names ([BDR-002](../business/002-neo4j-crd-topology.md) rejected Option C).

Implementers need a single authoritative mapping for the reconciler’s Helm adapter layer.

---

## Decision

We will translate Helm `values.yaml` into **`Neo4j` spec domain fields** via the tables below. The reconciler MUST NOT expose Helm keys on the CR.

### Topology

| Helm | `Neo4j` spec |
|------|--------------|
| `minimumClusterSize: 1`, single server | `mode: Standalone` |
| `minimumClusterSize: 3`, symmetric HA | `mode: Cluster`, `primaries.members: 3` |
| Read scaling | `secondaries.members: N` |
| `analytics.enabled: true`, GDS secondary | `analytics.members: N` |
| Primary + secondary + analytics (multi-release) | one CR — `primaries` + `secondaries` + `analytics` |
| `operations.enableServer: true` | operator enable-server job on scale-out |

### Identity

| Helm | `Neo4j` spec |
|------|--------------|
| `neo4j.edition` | `spec.edition` |
| `neo4j.acceptLicenseAgreement` | `spec.license.accept` |
| `image.registry` + `image.repository` + `image.tag` | `spec.image.registry`, `spec.image.repository`, `spec.version` |
| `image.customImage` | `spec.image.customImage` (full override; mutually exclusive with repository+version) |

### Auth

| Helm | `Neo4j` spec |
|------|--------------|
| `neo4j.password` | `auth.generatePassword: true` (operator generates) |
| `neo4j.passwordFromSecret` | `auth.passwordSecretRef.name` |
| LDAP settings in `config` / env | `auth.ldap.*` |

### Plugins

| Helm | `Neo4j` spec |
|------|--------------|
| `env` / `NEO4J_PLUGINS` | role `plugins` lists + operator-computed env |
| `apoc_config` | `pluginDefinitions.apoc.config` |
| `apoc_credentials` | `pluginDefinitions.apoc-extended.credentials[]` |
| GDS license Secret mount | `pluginDefinitions.gds.licenseSecretRef` |

### Persistence

See [ADR-003](003-persistence-model.md).

### TLS

| Helm | `Neo4j` spec |
|------|--------------|
| `ssl.bolt\|https\|cluster` | `trust.certificates.bolt\|https\|cluster` |
| `ssl.*.trustedCerts.sources` | `trust.certificates.*.trustedCerts` |
| `config.dbms.security.tls_reload_enabled` | `trust.reload.enabled` |

### Connectivity & monitoring

| Helm | `Neo4j` spec |
|------|--------------|
| `services.neo4j.*` | `connectivity.internal` / `connectivity.external` |
| Load balancer subchart values | `connectivity.external.*` |
| `serviceMonitor.*` | `monitoring.serviceMonitor.*` |
| Prometheus scrape config | `monitoring.prometheus.*` |

### Scheduling & ops

| Helm | `Neo4j` spec |
|------|--------------|
| `nodeSelector`, `affinity`, `tolerations`, `topologySpreadConstraints` | `spec.scheduling.*` |
| `podDisruptionBudget.*` | `spec.podDisruptionBudget.*` |
| `readinessProbe`, `livenessProbe`, `startupProbe` | `spec.probes.*` |
| `securityContext`, `containerSecurityContext` | `spec.security.*` |
| `neo4j.offlineMaintenanceModeEnabled` | `spec.maintenance.offlineMode` |
| neo4j-admin CronJob patterns | `spec.maintenance.jobs[]` |

### Config

| Helm | `Neo4j` spec |
|------|--------------|
| `config:` map | `spec.config` |
| Cluster discovery keys | operator-injected; user cannot override |

---

## Consequences

### Positive

- Migration guide is one table — no mental model of Helm multi-release.
- Reconciler owns translation in `internal/adapter/helm` — CR stays stable if Helm renames internal keys.

### Negative

- Helm-only users must learn role counters — offset by license-aligned `analytics.members` ([BDR-002](../business/002-neo4j-crd-topology.md)).

### Neutral

- `analysis/helm_neo4j_values.yaml` remains reference input; this ADR is normative for mapping.

---

## References

- [BDR-002](../business/002-neo4j-crd-topology.md)
- [ADR-003](003-persistence-model.md)
- [`helm-charts/neo4j/values.yaml`](../../../../helm-charts/neo4j/values.yaml)
