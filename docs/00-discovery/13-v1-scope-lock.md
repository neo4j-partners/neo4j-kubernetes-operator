# V1 scope lock — Neo4j operator MVP

Frozen commitment for **V1**: minimal `Neo4j` CRD operator — deploy Standalone or Cluster, reconcile lifecycle, scale — using the **simplest supported variant** of each accepted BDR.

**Derived from**: `01-functional_requirements.csv` (`V1=Yes`) + accepted BDRs (`design/decision-records/readme.md`).

**Status**: draft — aligned with implemented storage (BDR-005) and FR CSV 2026-07-17.

---

## Principles

1. **Neo4j CRD only** — V1 delivers the `Neo4j` workload operator; no backup/restore CRs, no neo4j-admin chart integration.
2. **Simplest path wins** — when a BDR accepts multiple modes, V1 implements one variant; other fields stay in the CRD but are **not V1-supported**.
3. **Backup workflows out of V1** — `NEO-2-013`, `NEO-2-014`, `features.backup`, backup port deferred; **`volumes.backups` mount is in scope** ([BDR-005]).
4. **CRD ≥ implementation** — deferred options (cloud object storage, Ingress, backup CRDs, cert-manager) remain in OpenAPI for forward compatibility but are not V1-supported.

---

## V1 includes (`V1=Yes`)

### Operator

| Area | V1 variant | BDR |
|------|------------|-----|
| Install | YAML manifests in dedicated operator namespace (`neo4j-operator-system`) | [BDR-003] |
| Scope | Single namespace (watch = operator namespace) | [BDR-003] |
| Reconcile + basic status | Ready / Installed / Error | — |
| RBAC | Namespace-scoped | [BDR-003] |
| Uninstall | Preserve PVCs | — |

### Neo4j workload

| Area | V1 variant | BDR |
|------|------------|-----|
| CRD | Single `Neo4j` kind | [BDR-001] |
| Topology | Standalone + Cluster (primaries / analytics / read) | [BDR-002] |
| Scale | Per-pool StatefulSet + ENABLE SERVER | [BDR-009] |
| Plugins | `pluginDefinitions` + pool refs | [BDR-004] |
| Config | `spec.config` passthrough + default JVM | [BDR-008] |
| Storage | Data `Dynamic`/`Existing`; aux `Share`/`Dynamic`/`Existing`; `additionalMounts` / `secretMounts` | [BDR-005] |
| Networking | ClusterIP / NodePort / LoadBalancer; HTTP + Bolt (+ HTTPS); internals derived in Cluster | [BDR-007] |
| TLS | BYO via `spec.trust` — cluster (Cluster mode); bolt/https (Cluster + Standalone) | [BDR-006] |
| Auth | Generated or existing password Secret | — |
| Scheduling | `spec.scheduling` wired to STS | — |
| Probes | Operator defaults or `spec.probes` override | — |
| Config change | Controlled / rolling restart | — |

---

## V1 excludes (explicit non-goals)

| Domain | FR IDs | Notes |
|--------|--------|-------|
| **Backup / restore workflows** | NEO-2-013, NEO-2-014, NEO-3-013-*, NEO-3-014-* | CRDs / jobs post-V1; `volumes.backups` mount is in V1 |
| **Monitoring ServiceMonitor / CSV/JMX/Graphite** | NEO-2-015, NEO-3-015-* (except volume) | Prometheus scrape + ServiceMonitor post-V1; `volumes.metrics` mount is in V1 |
| **Cloud object storage** | NEO-3-006-CLD-01/02 | Workload identity / cloud creds post-V1 |
| **Neo4j version upgrade** | NEO-2-012, NEO-3-012-* | `spec.version` at install only |
| **Ingress / reverse proxy** | NEO-3-007-PRT-*, ingress rules | post-V1 |
| **cert-manager / TLS reload** | NEO-3-005-TLS-04 | BYO Secrets only in V1 |
| **Logging customization** | NEO-2-016, NEO-3-016-* | Neo4j defaults; `volumes.logs` mount is in V1 |
| **Maintenance jobs** | NEO-2-017, NEO-3-017-* | post-V1 |
| **Operator Helm / multi-scope / upgrade** | OP-1-004, OP-1-007, OP-2-001-PKG-02, SCOPE-02/03 | post-V1 |

---

## MVP `Neo4j` spec sketch

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
spec:
  edition: enterprise
  version: "2026.05.0"
  license: { accept: "yes" }
  topology:
    mode: Cluster          # or Standalone
    primaries: { members: 3 }
    secondaries:
      read: { members: 1 }
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 100Gi
          storageClassName: gp3
  connectivity:
    listeners:
      bolt: 7687
      http: 7474
    service:
      type: ClusterIP
      expose:
        - bolt
        - http
  trust:                     # optional BYO TLS
    certificates:
      cluster:
        privateKey: { secretName: ..., subPath: ... }
        publicCertificate: { secretName: ..., subPath: ... }
  config: {}                 # optional passthrough
  # jvm under config in current CRD — see api-cheatsheet
```

---

## Next steps

- [x] Align `02-acceptance_criteria_library.csv` V1 flags with `01-functional_requirements.csv`
- [x] Align `04-test_catalog.csv` V1 flags with FR + AC library
- [x] Align storage FRs / scope lock with BDR-005 implementation (Existing, aux, mounts)
- [ ] Filter `04-test_catalog.csv` P0 gate to `V1=Yes` only for release validation
