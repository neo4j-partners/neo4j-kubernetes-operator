---
name: helm-template-tracer
description: >-
  Traces Neo4j Helm values to Kubernetes templates and Go code. Follows template
  conditionals to link scattered values.yaml paths to the same semantic concern
  (e.g. analytics at file end with minimumClusterSize via neo4j-config.yaml).
  Use when documenting Helm implementation or discovering cross-cutting topology.
---

# Helm template tracer

## Two tracing modes

| Mode | When | Goal |
|------|------|------|
| **Direct** | Single `helm_path` | values → template → K8s → neo4j.conf |
| **Conditional** | Topology, plugins, TLS | Find shared `if` branches linking distant values |

For conditional mode, also run **helm-semantic-concern-mapper**.

## Per helm_path workflow (direct)

1. **Search templates**

```bash
rg -l "\.Values\.<path>" helm-charts/neo4j/templates/
rg "<pathSegment>" helm-charts/internal/unit_tests/
```

Convert `neo4j.password` → `.Values.neo4j.password` or nested tpl access.

2. **Search Go model** — `helm-charts/internal/model/release_values.go`

3. **List K8s kinds** produced

4. **Trace runtime effect** — ConfigMap, env `NEO4J_*`, Jobs, init containers

## Conditional tracing (topology exemplar)

**Problem**: `analytics` is at **end** of `values.yaml`; `minimumClusterSize` is under `neo4j:` at **top** — same concern, different YAML regions.

**Procedure**:

1. Read `neo4j-config.yaml` top — variables:
   - `$clusterEnabled` ← `neo4j.isClusterEnabled` (minimumClusterSize ≥ 3)
   - `$primaryAnalyticsType` ← `analytics.enabled` + `type.name == primary`
   - `$secondaryAnalyticsType` ← `analytics.enabled` + `type.name == secondary`

2. Follow each branch's `neo4j.conf` keys (e.g. `initial.dbms.default_primaries_count`, `SECONDARY` mode_constraint)

3. Cross-reference `neo4j-statefulset.yaml` — note `replicas: 1` (Helm multi-member ≠ one STS scale)

4. Record **co-values** in field doc under **Semantic concerns**

```bash
rg 'analytics|minimumClusterSize|clusterEnabled|primaryAnalytics|secondaryAnalytics' \
  helm-charts/neo4j/templates/
```

## Key template map

| Area | Primary templates |
|------|-------------------|
| **Topology / cluster** | `neo4j-config.yaml`, `_helpers.tpl`, `neo4j-statefulset.yaml`, `neo4j-svc.yaml` |
| Workload | `neo4j-statefulset.yaml`, `neo4j-env.yaml` |
| Services | `neo4j-svc.yaml`, `_loadbalancer.tpl` |
| TLS | ssl tpl / statefulset mounts |
| Storage | `_volume.tpl` |
| Operations | operations job templates |

## Output (for field doc)

```markdown
## Helm implementation
- **Templates**: ...
- **Go model**: ...
- **K8s resources**: ...
- **Neo4j mechanism**: ...
- **Conditional links**: shares template branch with `<other helm_path>` in `neo4j-config.yaml` L...

## Semantic concerns
| concern_id | role | co-paths |
|------------|------|----------|
| CONCERN-TOPOLOGY | ... | analytics.*, neo4j.minimumClusterSize, ... |
```

## Tests as documentation

`helm-charts/internal/resources/testData/*.yaml` — gds, cluster, analytics scenarios.
