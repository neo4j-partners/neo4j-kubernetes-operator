# Quickstart — Cluster Neo4j

Deploy a multi-member Neo4j cluster (Enterprise) with primary and optional secondary pools.

Assumes the operator is already installed. Standalone path: [01-quickstart-standalone.md](01-quickstart-standalone.md).

## Sample manifest

[`config/samples/neo4j_v1beta1_neo4j_cluster.yaml`](../../../config/samples/neo4j_v1beta1_neo4j_cluster.yaml)

It defines:

- `topology.mode: Cluster`
- Primary + analytics (GDS/Bloom) + read pool
- `pluginDefinitions` for licensed plugins
- Dynamic data volume
- ClusterIP connectivity (bolt + http)
- Optional BYO cluster TLS via `spec.trust`

## Apply

Sample omits `metadata.namespace` — deploys to **`default`**:

```bash
kubectl apply -f config/samples/neo4j_v1beta1_neo4j_cluster.yaml
kubectl get neo4j -n default -w
```

Expect one StatefulSet per pool, headless + client Services, and `status.conditions[Ready]` once members form.

Storage variants (Existing, aux volumes): [`examples/storage/`](../../../examples/storage/).

## Clean up

```bash
kubectl delete -f config/samples/neo4j_v1beta1_neo4j_cluster.yaml
```

PVCs may remain until explicitly deleted — see [Uninstall](../operator/03-uninstall.md).
