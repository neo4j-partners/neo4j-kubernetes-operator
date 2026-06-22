# Security examples

Drop-in YAML for hardening Neo4j deployments. None of these are deployed
automatically by the operator — apply them per-namespace, after you've
created your `Neo4jEnterpriseCluster` / `Neo4jEnterpriseStandalone` CR.

## NetworkPolicies

| File | Use case |
|---|---|
| [`networkpolicy-cluster.yaml`](networkpolicy-cluster.yaml) | Per-cluster ingress + egress rules. Allows Bolt/HTTP from same namespace, intra-cluster discovery (6000) + RAFT (7000) between server pods, Prometheus scrape from the operator namespace. Replace `MY-CLUSTER` and `MY-NAMESPACE`. |
| [`networkpolicy-standalone.yaml`](networkpolicy-standalone.yaml) | Same shape, single-pod variant. No intra-cluster ports to allow. |

Apply with `kubectl apply -f` after editing the placeholders. Both policies start from a
default-deny posture (declaring `Ingress` and `Egress` in `policyTypes` with explicit
rules means everything not listed is denied) and add back only the traffic Neo4j
genuinely needs.

## What the operator itself ships

The operator's own NetworkPolicy is bundled in the Helm chart and gated on
`networkPolicy.enabled` in `values.yaml`. When enabled it allows Prometheus
scrape on the metrics endpoint and the operator's egress to Neo4j workload
pods, DNS, and the K8s API — see
[`charts/neo4j-operator/templates/networkpolicy.yaml`](../../charts/neo4j-operator/templates/networkpolicy.yaml).

Operator-managed per-cluster NetworkPolicies (i.e. the operator creates the
policy as a child resource of each `Neo4jEnterpriseCluster`) are a future
enhancement — the design is in the November 2025 security review's
recommendation #3. Until that lands, use the examples here.

## Other hardening already on by default

- Pod / container `SecurityContext` (RunAsNonRoot, drop ALL caps,
  RuntimeDefault seccomp) is applied to cluster, standalone, backup,
  restore, and plugin pods. See `internal/resources/security_context.go`.
- TLS for Bolt is opt-in via `spec.tls.mode: cert-manager` on the cluster
  / standalone CR. When enabled, the operator sets
  `server.bolt.tls_level=REQUIRED` and rejects plain `bolt://` clients.
- Plugin supply chain: `Neo4jPlugin.spec.source.checksum` is required for
  `type=url` and `type=custom`; SHA1/MD5 are rejected. See
  [`docs/api_reference/neo4jplugin.md`](../../docs/api_reference/neo4jplugin.md#supply-chain).
- Operator RBAC: the controller no longer requests `pods/exec` (stale from
  the old sidecar-exec backup architecture, removed May 2026).
