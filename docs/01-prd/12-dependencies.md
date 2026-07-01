# Dependencies

External systems, platform services, and internal design artefacts the operator relies on. **V1** dependencies are required for MVP; **V2+** are noted for roadmap continuity.

Technical platform matrix (CSI, IAM, LB per cloud) → [`../02-technical-design/dependencies.md`](../02-technical-design/dependencies.md).

---

## V1 runtime dependencies

| Dependency | Role | V1 requirement | Failure impact |
|------------|------|----------------|----------------|
| **Kubernetes** | API server, scheduler, kubelet | ≥ 1.27 (n-2 target); CI on kind | Operator cannot run |
| **Container runtime** | Pull Neo4j image | Any CRI supported by target K8s | Pods not created |
| **CSI / StorageClass** | Dynamic PVC for `volumes.data` | One StorageClass name in spec | Data volume not bound |
| **Neo4j Enterprise image** | Database process | Calver tag in `spec.version`; license accept | Workload not started |
| **Enterprise license** | Legal + runtime | `license.accept: "yes"` or license Secret | Admission / startup failure |

### Optional in V1 (MVP path does not require)

| Dependency | When needed | V1 |
|------------|-------------|-----|
| **cert-manager** | Automatic TLS issuance | No — BYO cluster certs ([`13-v1-scope-lock`](../00-discovery/13-v1-scope-lock.md)) |
| **Prometheus Operator** | ServiceMonitor scrape | No — monitoring deferred |
| **Cloud object store** (S3/GCS/Azure) | Backup / restore | No — backup deferred |
| **Ingress controller** | External HTTP routing | No — ClusterIP only |
| **Load balancer controller** | `type: LoadBalancer` | No — ClusterIP only |

---

## V1 design dependencies (internal)

| Artefact | Purpose |
|----------|---------|
| Accepted BDRs | API and behaviour contracts — [`../02-technical-design/decision-records/readme.md`](../02-technical-design/decision-records/readme.md) |
| [`crd-spec/neo4j/`](../02-technical-design/crd-spec/neo4j/) | OpenAPI, validation, status model |
| [`07-functional-requirements.csv`](07-functional-requirements.csv) | Traceable V1 scope |
| [`02-acceptance_criteria_library.csv`](02-acceptance_criteria_library.csv) | Testable outcomes |
| [`04-test-plan/`](../04-test-plan/) | Variant matrix + test catalog |

---

## V2+ dependencies (from reference PRD)

| Dependency | Capability | FR / notes |
|------------|------------|------------|
| **cert-manager** | TLS-01..04, cert rotation CW-7 | Post-V1 client + auto rotation |
| **Cloud IAM** (IRSA / WI / Azure MI) | Backup to object store without static keys | NEO-2-013, reference F-4 |
| **Object storage** | S3, GCS, Azure Blob | Backup / restore CRDs |
| **Prometheus / Grafana** | Metrics dashboards | NEO-2-015, NFR-OBS-001 |
| **OpenTelemetry collector** | Distributed traces | NFR-OBS-002 |
| **LDAP / SSO secrets** | External auth provider config | Reference F-6; not full directory sync |

---

## Organizational dependencies

| Dependency | Owner | Blocks |
|------------|-------|--------|
| **Product Engineering** sponsorship | Product | GA, long-term maintenance |
| **Support** tier definition | Product + Support | Enterprise adoption |
| **Neo4j release train** alignment | Product Engineering | Image compatibility matrix |

See [`11-risks.md`](11-risks.md) and [`14-open-questions.md`](14-open-questions.md).

---

## Dependency diagram (V1)

```
                    ┌─────────────────┐
                    │  Kubernetes API │
                    └────────┬────────┘
                             │
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
┌────────────────┐  ┌───────────────┐  ┌─────────────────┐
│ Neo4j Operator │  │  StorageClass │  │ Neo4j Enterprise│
│ (YAML install) │  │  + CSI driver │  │     image       │
└───────┬────────┘  └───────┬───────┘  └────────┬────────┘
        │                   │                    │
        └───────────────────┼────────────────────┘
                            ▼
                   ┌────────────────┐
                   │  Neo4j CR      │
                   │  → STS/SVC/PVC │
                   └────────────────┘
```

Backup, ingress, cert-manager, and fleet watch modes attach at V1.1 / V2 — not shown.
