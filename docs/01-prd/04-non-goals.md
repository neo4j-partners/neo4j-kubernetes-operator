# Non-goals

Explicit **out-of-scope** items for V1. The CRD may expose fields for forward compatibility; **V1 does not implement, test, or document** these paths unless noted.

Source of truth for engineering: [`../02-technical-design/13-v1-scope-lock.md`](../02-technical-design/13-v1-scope-lock.md) and `V1=No` in [`07-functional-requirements.csv`](07-functional-requirements.csv).

---

## V1 non-goals

### Workloads & day-2 CRDs

| Non-goal | Rationale |
|----------|-----------|
| **Backup** (`Neo4jBackup`, schedules, `features.backup`, backup port) | Deferred entire domain — post-V1 |
| **Restore** (`Neo4jRestore`, seed URI) | Depends on backup; post-V1 |
| **`Neo4jDatabase`** logical database CRD | Post-V1 |
| **`Neo4jUser`**, **`Neo4jRole`**, **`Neo4jGrant`** | Post-V1 — declarative native RBAC ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md)) |
| **Maintenance jobs** (dump/load, consistency check, offline mode) | Post-V1 |

### Observability & ops extras

| Non-goal | Rationale |
|----------|-----------|
| **Monitoring / Prometheus / ServiceMonitor** | Post-V1 ([BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md)) |
| **Operator metrics & structured observability** (`OP-1-007`) | Post-V1 |

### Neo4j & platform variants

| Non-goal | Rationale |
|----------|-----------|
| **Neo4j rolling version upgrade** | `spec.version` set at install only in V1 |
| **Community edition** | Enterprise only for V1 |
| **Evaluation license path** | Post-V1 |

### Storage & networking (non-MVP variants)

| Non-goal | Rationale |
|----------|-----------|
| **Existing PVC / selector / volumeClaimTemplate** binding modes | Dynamic data volume only |
| **Auxiliary volumes** (backups, logs, metrics, import, licenses mounts) | Post-V1 |
| **LoadBalancer / NodePort** client Services | ClusterIP only |
| **HTTPS, Bolt TLS, Ingress** | Plain HTTP + Bolt for MVP |
| **Multi-cluster K8s exposure** | `multiCluster.enabled: false` only |

### Security & scheduling extras

| Non-goal | Rationale |
|----------|-----------|
| **Custom scheduling** (affinity, tolerations, topology spread, priority class) | Kubernetes defaults |
| **Arbitrary secret mounts / LDAP** | Minimal auth path only |
| **TLS reload** | Post-V1 |
| **cert-manager** as default TLS path | BYO secrets; cert-manager optional post-V1 |

### Operator packaging & scope

| Non-goal | Rationale |
|----------|-----------|
| **Operator Helm chart** | YAML install only |
| **Multi-namespace / cluster-wide** operator scope | Single namespace ([BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md)) |
| **Operator self-upgrade** workflow | Post-V1 |
| **CRD conversion webhook** | V1.1+ |

### Product & organizational

| Non-goal | Rationale |
|----------|-----------|
| **PS-only long-term ownership** without Product Engineering | Not sustainable for GA — see [`11-risks.md`](11-risks.md) |
| **Full Helm feature parity on day one** | MVP first; parity via phased roadmap |

---

## General non-goals (all versions)

- Replacing Neo4j Server or changing database semantics.
- Managing non-Kubernetes infrastructure (VM bare-metal, non-K8s orchestrators).
- Being a generic graph-database operator framework.
