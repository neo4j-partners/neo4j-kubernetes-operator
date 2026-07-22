# API cheatsheet

Short reference for operators and SREs. **Authoritative spec:** [`crd-spec/neo4j/spec.md`](../../02-technical-design/crd-spec/neo4j/spec.md).

## Resource

| Property | Value |
|----------|-------|
| API group | `neo4j.com` |
| Version | `v1beta1` |
| Kind | `Neo4j` |
| Short name | `n4j` |
| Scope | Namespaced |

```bash
kubectl get neo4j
kubectl get n4j
kubectl describe neo4j <name> -n <namespace>
```

## Minimal Standalone (V1)

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: my-graph
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

## Key spec sections

| Section | Purpose |
|---------|---------|
| `edition`, `version`, `license` | Identity — V1: `enterprise` + `accept: "yes"` |
| `topology` | `Standalone` or `Cluster` (+ pools for Cluster) |
| `storage` | Volumes, extra mounts, secret mounts ([BDR-005](../../02-technical-design/decision-records/business/neo4j/005-storage-volume-mode.md)) |
| `storage.volumes.data` | **Required** — `Dynamic` or `Existing` (`claimName` / `volume` / `volumeClaimTemplate`) |
| `storage.volumes.*` | Aux: `backups`, `logs`, `metrics`, `import`, `licenses` — `Share` / `Dynamic` / `Existing` |
| `storage.additionalMounts` / `secretMounts` | Escape-hatch mounts |
| `config.neo4j` | `neo4j.conf` drop-in keys |
| `config.apoc` | `apoc.conf` drop-in keys |
| `config.jvm` | JVM arguments |
| `auth` | Generated or referenced password Secret |
| `connectivity` | Listen ports, Services, Ingress (Ingress deferred) |
| `trust` | BYO TLS for bolt / https / cluster |
| `scheduling` | nodeSelector, affinity, tolerations, … |
| `features` | Backup connector, monitoring prometheus + ServiceMonitor (CSV/JMX/Graphite deferred) |

## Status (automation)

Gate on **conditions**, not `phase` alone:

| Condition | Meaning |
|-----------|---------|
| `Ready` | Workload reachable for current generation |
| `Installed` | Base K8s objects exist |
| `Error` | Irrecoverable reconcile failure |
| `Reconciling` | Active reconcile |
| `StorageReady` | Data PVCs bound |

Useful fields:

- `status.endpoints.bolt` / `status.endpoints.http` — client URIs
- `status.credentials.secretName` — auth Secret reference (not the password)
- `status.serverSummary.ready` / `servers` — replica counts

Full status model: [`status.md`](../../02-technical-design/crd-spec/neo4j/status.md)

## Samples

| File | Mode |
|------|------|
| [`config/samples/neo4j_v1beta1_neo4j.yaml`](../../../config/samples/neo4j_v1beta1_neo4j.yaml) | Standalone |
| [`config/samples/neo4j_v1beta1_neo4j_cluster.yaml`](../../../config/samples/neo4j_v1beta1_neo4j_cluster.yaml) | Cluster (preview) |

## V1 not supported (do not rely on)

See [V1 scope lock](../../00-discovery/13-v1-scope-lock.md):

- Cluster mode (Slice 2)
- TLS / cert-manager (Slice 3)
- `features.backup`, `features.monitoring`
- LoadBalancer / Ingress (V1.1+)
- Multi-namespace operator watch
