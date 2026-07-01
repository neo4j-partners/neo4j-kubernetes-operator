# API overview

Kubernetes API surface for the Neo4j operator. **Authoritative field definitions** live in [`../02-technical-design/crd-spec/`](../02-technical-design/crd-spec/).

This document summarises **kinds, phasing, and mapping** from the reference operator PRD ([`../00-discovery/export.md`](../00-discovery/export.md)) to **this project's decisions**.

---

## Design principles

| Principle | This project | Reference PRD (export) |
|-----------|--------------|------------------------|
| Workload CRD | Single **`Neo4j`** kind with `topology.mode` | Separate **`Neo4jCluster`** kind |
| Infra on workload | `spec.volumes`, `spec.connectivity`, `spec.trust`, `spec.config` embedded | Flat `Neo4jCluster` fields (`storage`, `tls`, `service`) |
| V1 scope | **`Neo4j` only** | Cluster + Backup + User/Role/Grant + UI |
| API version | `neo4j.com/v1beta1` (target) | `neo4j.com/v1alpha2` in reference appendix |

**[BDR-001](../02-technical-design/decision-records/business/001-single-neo4j-crd.md)** — accepted: one `Neo4j` CRD, not `Neo4jStandalone` + `Neo4jCluster`.

---

## V1 API (`V1=Yes`)

### `Neo4j` — primary workload CRD

| | |
|---|---|
| **Group** | `neo4j.com` |
| **Kind** | `Neo4j` |
| **Scope** | Namespaced |
| **Reconciler** | `Neo4jReconciler` |

**Spec sections (V1-used):**

| Section | Purpose | V1 subset |
|---------|---------|-----------|
| `edition`, `version`, `license` | Enterprise + calver at install | Full |
| `topology` | Standalone or Cluster; primaries / analytics / read pools | Full |
| `volumes.data` | Dynamic PVC | `mode: Dynamic` only |
| `connectivity` | Listeners + client Service + ingress (deferred) | ClusterIP; HTTP + Bolt; `listeners` + `service` |
| `trust` | TLS material | Cluster policy BYO certs when `mode: Cluster` |
| `config`, `jvm` | Runtime settings | Passthrough + default JVM |
| `pluginDefinitions` + pool `plugins` | APOC, GDS, Bloom | Per [BDR-004](../02-technical-design/decision-records/business/004-neo4j-plugin-topology.md) |

**Status (V1):** `Ready`, `Installed`, `Error` conditions — see [`crd-spec/neo4j/status.md`](../02-technical-design/crd-spec/neo4j/status.md).

**Examples:** [`crd-spec/neo4j/example.yaml`](../02-technical-design/crd-spec/neo4j/example.yaml), [`example-cluster.yaml`](../02-technical-design/crd-spec/neo4j/example-cluster.yaml).

**Reference PRD mapping:** `Neo4jCluster` → `Neo4j` with `topology.mode: Cluster`; `image.repo/tag` → `spec.version` + operator image catalog; `storage.className/size` → `volumes.data.dynamic`; `tls` → `spec.trust`; `service` → `spec.connectivity.service`.

---

## Post-V1 API (designed, not V1-tested)

CRD folders exist for forward design; **`V1=No`** in [`07-functional-requirements.csv`](07-functional-requirements.csv).

| Kind | Folder | Purpose | FR domain |
|------|--------|---------|-----------|
| `Neo4jDatabase` | [`neo4jdatabase/`](../02-technical-design/crd-spec/neo4jdatabase/) | Logical database CREATE/DROP | Post-V1 |
| `Neo4jBackup`, `Neo4jBackupSchedule` | [`neo4jbackup/`](../02-technical-design/crd-spec/neo4jbackup/) | Scheduled / on-demand backup | NEO-2-013 |
| `Neo4jRestore` | [`neo4jrestore/`](../02-technical-design/crd-spec/neo4jrestore/) | Restore from backup / seed | NEO-2-014 |

Also activates `spec.features.backup` and `connectivity.listeners.backup` when implemented ([BDR-010](../02-technical-design/decision-records/business/010-neo4j-features-catalog.md)).

---

## Roadmap API (reference PRD — [BDR-012](../02-technical-design/decision-records/business/012-identity-management.md))

Items from the external PRD and operator proposal. **Post-V1** — see BDR-012 for the accepted direction (proposed).

| Kind / surface | Reference capability | BDR-012 |
|----------------|---------------------|---------|
| `Neo4jUser` | Declarative DB users | Option C — `passwordSecretRef`, role bindings |
| `Neo4jRole` | Declarative roles | Option C — inline `privileges[]` |
| `Neo4jGrant` | Fine-grained grants | Option C — `statements[]`, `whenNotMatched` |
| Web UI | Wizards for cluster / backup / security | Product roadmap — not in BDR-012 |
| Operator Helm chart | `helm install neo4j-operator` | OP-2-001-PKG-02 deferred |

---

## Admission & validation

| Mechanism | V1 | Detail |
|-----------|-----|--------|
| OpenAPI schema | Yes | CRD structural validation |
| CEL rules | Yes | Topology, connectivity, license, LISTENER/CFG-LISTENER rules |
| Mutating / validating webhooks | Yes | Defaults, denylist, contradiction checks |

See [`crd-spec/neo4j/validation.md`](../02-technical-design/crd-spec/neo4j/validation.md).

**Reference PRD targets:** webhook P95 < 500 ms; odd primary count; username regex — adopt for V2 security CRDs; V1 validates topology and connectivity coherence.

---

## Operator installation API (not a CRD)

V1 installs via **YAML manifests** in the operator namespace:

- Deployment (single replica)
- ServiceAccount, Role, RoleBinding (namespace scope)
- CRD + webhook configurations

No `OperatorConfig` CRD in V1. Multi-namespace watch modes (`--watch-mode`) deferred ([BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md)).
