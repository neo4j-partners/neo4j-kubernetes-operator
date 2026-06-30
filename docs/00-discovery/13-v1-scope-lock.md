# V1 scope lock — Neo4j operator MVP

Frozen commitment for **V1**: minimal `Neo4j` CRD operator — deploy Standalone or Cluster, reconcile lifecycle, scale — using the **simplest supported variant** of each accepted BDR.

**Derived from**: `01-functional_requirements.csv` (`V1=Yes`) + accepted BDRs (`design/decision-records/readme.md`).

**Status**: draft — aligned with FR CSV review 2026-06-22.

---

## Principles

1. **Neo4j CRD only** — V1 delivers the `Neo4j` workload operator; no backup/restore CRs, no neo4j-admin chart integration.
2. **Simplest path wins** — when a BDR accepts multiple modes, V1 implements one variant; other fields stay in the CRD but are **not V1-supported**.
3. **Backup out of V1** — `NEO-2-013`, `NEO-2-014`, `features.backup`, backup port, backup volumes deferred.
4. **CRD ≥ implementation** — deferred options remain in OpenAPI for forward compatibility ([BDR-005] Existing modes, [BDR-007] LoadBalancer, etc.) but are not tested or documented as V1.

---

## V1 includes (`V1=Yes`)

### Operator

| Area | V1 variant | BDR |
|------|------------|-----|
| Install | YAML manifests | [BDR-003] |
| Scope | Single namespace | [BDR-003] |
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
| Storage | `volumes.data.mode: Dynamic` + `storageClassName` + `size` | [BDR-005] |
| Networking | ClusterIP; HTTP + Bolt; internals derived in Cluster | [BDR-007] |
| TLS | Cluster policy BYO certs when `mode: Cluster` only | [BDR-006] |
| Auth | Generated or existing password Secret | — |
| Probes | Operator defaults | — |
| Config change | Controlled / rolling restart | — |

---

## V1 excludes (explicit non-goals)

| Domain | FR IDs | Notes |
|--------|--------|-------|
| **Backup / restore** | NEO-2-013, NEO-2-014, NEO-3-013-*, NEO-3-014-* | Entire domain post-V1 |
| **Monitoring / Prometheus** | NEO-2-015, NEO-3-015-* | `features.monitoring` post-V1 ([BDR-010]) |
| **Neo4j version upgrade** | NEO-2-012, NEO-3-012-* | `spec.version` at install only |
| **Custom scheduling** | NEO-2-008, NEO-3-008-* | K8s defaults |
| **LoadBalancer / NodePort** | NEO-3-007-SVC-02/03 | ClusterIP only |
| **HTTPS / Bolt TLS / ingress** | NEO-3-005-TLS-01/02, NEO-3-007-PRT-02, PCMB-04+ | Plain HTTP+Bolt MVP |
| **Storage modes other than Dynamic** | NEO-3-006-PVC-01/03/04/05, aux volumes | Existing/Share/selector post-V1 |
| **Logging customization** | NEO-2-016, NEO-3-016-* | Neo4j defaults |
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
  trust:                     # Cluster mode only
    certificates:
      cluster:
        privateKey: { secretName: ..., subPath: ... }
        publicCertificate: { secretName: ..., subPath: ... }
  config: {}                 # optional passthrough
  jvm:
    useDefaults: true
```

---

## Next steps

- [x] Align `02-acceptance_criteria_library.csv` V1 flags with `01-functional_requirements.csv`
- [x] Align `04-test_catalog.csv` V1 flags with FR + AC library
- [ ] Filter `04-test_catalog.csv` P0 gate to `V1=Yes` only for release validation
