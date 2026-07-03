# Quickstart — Cluster Neo4j (preview)

> **Not yet supported in Slice 1.** The operator returns an error for `topology.mode: Cluster` until Slice 2 (multi-pool StatefulSets, formation). This page documents the **target** sample for when Cluster support lands.

## Sample manifest

The repository includes a Cluster example for design and future testing:

[`config/samples/neo4j_v1beta1_neo4j_cluster.yaml`](../../../config/samples/neo4j_v1beta1_neo4j_cluster.yaml)

It defines:

- `topology.mode: Cluster`
- Primary + analytics (GDS/Bloom) + read pool
- `pluginDefinitions` for licensed plugins
- Dynamic 100Gi data volume
- ClusterIP connectivity (bolt + http)

**Do not apply this sample** until Cluster reconciliation is implemented.

## When available (Slice 2+)

Expected workflow (sample omits `metadata.namespace` — deploys to **`default`**):

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j_cluster.yaml
kubectl get neo4j prod -n default -w
```

Expected pools ([BDR-009](../../02-technical-design/decision-records/business/neo4j/009-scale-pool-ordinal-semantics.md)):

| Pool | StatefulSet |
|------|-------------|
| Primary | `{name}-primary` |
| Analytics | `{name}-analytics` |
| Read | `{name}-read` (scalable via `scale` subresource) |

## Design references

- Topology: [BDR-002](../../02-technical-design/decision-records/business/neo4j/002-neo4j-crd-topology.md)
- Plugins: [BDR-004](../../02-technical-design/decision-records/business/neo4j/004-neo4j-plugin-topology.md)
- Example (design): [`example-cluster.yaml`](../../02-technical-design/crd-spec/neo4j/example-cluster.yaml)

## Supported today

Use [Quickstart — Standalone](01-quickstart-standalone.md) instead.
