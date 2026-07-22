# Roadmap — V1 / V2 phasing

Product phasing for the Neo4j Kubernetes Operator. Engineering scope detail → [`../02-technical-design/13-v1-scope-lock.md`](../02-technical-design/13-v1-scope-lock.md).

---

## V1 — MVP (current commitment)

**Theme**: One `Neo4j` CRD, minimal happy path, production-leaning cluster operations without backup.

| Track | Delivers |
|-------|----------|
| **Operator** | YAML install, single-namespace, reconcile, basic status, RBAC, uninstall (preserve data) |
| **Neo4j workload** | Standalone + Cluster, per-pool STS, scale, config change restart |
| **Storage** | `volumes.data` Dynamic + `storageClassName` |
| **Network** | ClusterIP, HTTP + Bolt, internals in Cluster |
| **Security** | Enterprise license, password Secret, cluster TLS (BYO) |
| **Plugins** | `pluginDefinitions` + pool refs |

**Explicitly not in V1**: backup, restore, monitoring, Neo4j upgrade, LB/NodePort, HTTPS, custom scheduling, day-2 CRDs.

Requirements: `V1=Yes` in [`07-functional-requirements.csv`](07-functional-requirements.csv).  
Tests: `V1=Yes` in [`../02-technical-design/04-test_catalog.csv`](../02-technical-design/04-test_catalog.csv).

### V1 exit criteria (draft)

- [ ] All V1 P0 tests pass on reference platform (kind + one cloud)
- [ ] `13-v1-scope-lock.md` status = frozen
- [x] BDR-002 ratified (`accepted`); [ ] BDR-003 ratified
- [ ] Getting-started doc + 3 sample manifests (standalone, cluster, cluster + read scale)
- [ ] Product Engineering sponsorship decision recorded

---

## V1.1 — hardening & exposure

| Item | Notes |
|------|-------|
| Ingress / reverse proxy | [BDR-007](../02-technical-design/decision-records/business/neo4j/006-service-exposure-connectivity.md) Amendment F · [BDR-011](../02-technical-design/decision-records/business/neo4j/011-https-connector-tls-coupling.md) |
| `features.monitoring` CSV / JMX / Graphite | [BDR-010](../02-technical-design/decision-records/business/neo4j/010-neo4j-features-catalog.md) |
| Multi-namespace operator scope | [BDR-003](../02-technical-design/decision-records/business/003-operator-install-scope.md) |
| cert-manager TLS / certificate reload | [BDR-006](../02-technical-design/decision-records/business/neo4j/007-tls-trust-model.md) |

> **Shipped in MVP (removed from this table):** LoadBalancer/NodePort, BYO bolt/https/cluster TLS, `spec.scheduling` + custom probes, storage `Existing` / aux volumes / `secretMounts` ([BDR-005]), `features.monitoring.prometheus` + `serviceMonitor`.

---

## V2 — day-2 platform

| Item | Notes |
|------|-------|
| **Backup / restore** CRDs | `neo4jbackup`, `neo4jrestore`; `features.backup`; pod identity (reference F-3, F-4) |
| **Neo4j version upgrade** | Rolling upgrade workflow (reference CW-2) |
| **`Neo4jDatabase`** | Logical databases (reference F-13) |
| **Helm migration** | `11-helm-mapping.md` as supported path |
| **Maintenance jobs** | dump/load, consistency check |
| **Operator Helm chart** | `OP-2-001-PKG-02` |
| **Observability** | Prometheus metrics, OTEL traces (reference F-10, NFR-OBS) |

---

## V3+ — fleet & security (reference PRD direction)

| Item | Notes |
|------|-------|
| **Declarative identity CRDs** | `Neo4jUser`, `Neo4jRole`, `Neo4jGrant` ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md)) | Reference F-14–F-18 |
| **Optional Web UI** | Cluster / backup / security wizards (reference F-19–F-24) |
| **Multi-namespace / prefix watch** | Fleet operator (reference F-21–F-22) |
| **Blue/green major upgrades** | Reference non-goal N-2 follow-up |

---

## Sequencing

Detailed implementation order → [`../02-technical-design/17-roadmap.md`](../02-technical-design/17-roadmap.md) *(technical milestones)*.  
Effort estimates → [`../02-technical-design/19-delivery-estimate.md`](../02-technical-design/19-delivery-estimate.md).
