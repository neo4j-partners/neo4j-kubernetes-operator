# Connectivity

Two layers, and keeping them apart makes the rest obvious. **Listeners** are ports Neo4j opens inside
the pod. **Services** are how Kubernetes exposes some of those ports to clients. You configure them
separately, because "Neo4j speaks HTTPS" and "the world can reach HTTPS" are different decisions.

## Listeners

```yaml
spec:
  connectivity:
    listeners:
      bolt: 7687
      http: 7474
      https: 7473
      backup: 6362
      metrics: 2004
```

Bolt and HTTP are always on, at 7687 and 7474 unless you change the port. The other three are off
until you name them — declaring the field is what enables the connector:

| Listener | Default port when enabled | Enabled by |
|----------|--------------------------|-----------|
| `bolt` | 7687 | Always on |
| `http` | 7474 | Always on |
| `https` | 7473 | Setting `listeners.https` |
| `backup` | 6362 | Setting `listeners.backup`, which also requires `features.backup.enabled: true` |
| `metrics` | 2004 | Setting `listeners.metrics`, which also requires `features.monitoring.prometheus.enabled: true` |

Those last two dependencies are enforced at admission: a listener without its feature is rejected
with a message naming the missing field, so you cannot end up with a port that nothing serves.

Listen addresses themselves are not yours to set. `server.bolt.listen_address`,
`server.http.enabled` and their siblings are rejected in `spec.config.neo4j` because the operator
derives them from these fields — see
[Operator-owned settings](../05-reference/operator-owned-config.md).

Serving HTTPS also needs certificates. Enabling the listener without TLS material gives you a port
that cannot complete a handshake; see [Security](05-security.md#transport-security).

## The client Service

One Service carries your application traffic, named after the resource. In Cluster mode it selects
the primary pool, which is what a routing driver needs to bootstrap.

```yaml
spec:
  connectivity:
    service:
      type: ClusterIP        # default
      expose: [bolt, http]   # default: bolt and http
```

`expose` is a filter over enabled listeners, so asking for a connector that is not enabled publishes
nothing rather than erroring. Port names follow the `tcp-bolt`, `tcp-http`, `tcp-https`,
`tcp-backup`, `tcp-prometheus` convention.

If you need the Service to answer on a different port than Neo4j listens on — fronting Bolt on 443,
for instance — remap it:

```yaml
spec:
  connectivity:
    service:
      ports:
        bolt: 443
```

### In-cluster access

Applications in the same cluster use the Service DNS name and need nothing else:

```
neo4j://dev.default.svc.cluster.local:7687
```

Use the `neo4j://` scheme rather than `bolt://` even for a single instance. It costs nothing on
Standalone and it is required for a cluster, where a direct connection may land on a member that does
not host your database. The operator publishes usable URIs and driver snippets under
`status.endpoints`:

```bash
kubectl get neo4j dev -n default -o jsonpath='{.status.endpoints.neo4j}{"\n"}'
kubectl get neo4j dev -n default -o jsonpath='{.status.endpoints.connectionExamples.python}{"\n"}'
```

### Port-forward, for humans

```bash
kubectl port-forward -n default svc/dev 7687:7687 7474:7474
```

Then `neo4j://localhost:7687` and `http://localhost:7474`. This is the right tool for a shell session
or Neo4j Browser and the wrong one for an application: the tunnel is single-client and dies with your
terminal.

### NodePort

```yaml
spec:
  connectivity:
    service:
      type: NodePort
```

Kubernetes allocates a high port on every node. Convenient on a lab cluster, awkward in production
because clients need node addresses and the ports are not the ones Neo4j drivers expect.
Example: [`examples/standalone/06-service-nodeport.yaml`](../../../examples/standalone/06-service-nodeport.yaml).

### LoadBalancer

```yaml
spec:
  connectivity:
    service:
      type: LoadBalancer
      loadBalancerSourceRanges:
        - 203.0.113.0/24
      annotations:
        service.beta.kubernetes.io/azure-load-balancer-internal: "true"
```

`loadBalancerSourceRanges` is **required** when the type is `LoadBalancer`, and admission rejects the
resource without it. The reason is blunt: on most cloud providers a LoadBalancer Service gets a public
address, and a Neo4j endpoint reachable from the whole internet is one password-guessing script away
from being a problem. Naming the ranges you trust makes exposure a deliberate act.

Anything provider-specific — internal load balancers, fixed addresses, health probe tuning — goes
through `annotations`, which the operator copies onto the Service unchanged.
Examples: [`examples/standalone/05-service-loadbalancer.yaml`](../../../examples/standalone/05-service-loadbalancer.yaml),
[`examples/cluster/04-service-loadbalancer.yaml`](../../../examples/cluster/04-service-loadbalancer.yaml).

Expose TLS-protected ports rather than plaintext ones when the load balancer is public: publish
`https` and Bolt with a certificate, per [Security](05-security.md#transport-security).

## The admin Service

Besides the client Service, the operator creates `<name>-admin` whenever a deployment is clustered, or
has the backup feature enabled, or has Prometheus enabled. It publishes every enabled connector,
including the operational ones you would not put in front of applications, and it is always
`ClusterIP`.

Use it for scraping metrics, for backup tooling, and for administrative sessions. Point applications
at the client Service instead — the admin Service intentionally selects members that may include
those you do not want serving user traffic.

## Headless Services and per-pod addresses

Each pool also has a headless Service, named after its StatefulSet, giving every pod a stable DNS
name of the form `dev-server-0.dev-server.default.svc.cluster.local`. That is how Neo4j members find
each other, and it is occasionally what you want for diagnostics — reaching one specific member with
`kubectl exec` and `cypher-shell`.

Cluster DNS is assumed to be `cluster.local`. On a cluster installed with another domain, set
`spec.connectivity.clusterDomain`, which affects the addresses Neo4j advertises to its peers and to
routing clients. See [Clustering](02-clustering.md#how-members-find-each-other).

## Not implemented yet

`spec.connectivity.ingress` and `spec.connectivity.reverseProxy` exist in the schema and are inert:
the operator renders nothing for them. Until they are implemented, expose Bolt and HTTP through a
Service, and put your own ingress in front of the HTTP Service if you need host-based routing —
noting that Bolt is not HTTP and most ingress controllers cannot route it without TCP passthrough.

`spec.connectivity.multiCluster.enabled: true` is rejected at admission.

## Checking what is exposed

```bash
kubectl get svc -n default -l app.kubernetes.io/instance=dev

kubectl get neo4j dev -n default -o jsonpath='{.status.endpoints}{"\n"}'
```

An empty external address on a LoadBalancer Service is a provider matter, not an operator one:
`kubectl describe svc` shows the events explaining why.

## Next

[Security](05-security.md) · [Monitoring](08-monitoring.md) ·
[Troubleshooting](../04-troubleshooting/01-common-issues.md)
