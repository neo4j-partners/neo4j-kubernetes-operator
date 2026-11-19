# Enterprise Studio

The `Neo4jStudio` CRD runs **Neo4j Enterprise Studio** and points it at one or more `Neo4j`
deployments. The operator reconciles it into a stateless `Deployment` (image `neo4j/enterprise-studio`),
a `Service`, and an optional `Ingress`; every setting is rendered as `NES_*` environment variables
(the documented way to override the image's bundled `config.yaml`), and credentials ride as
`secretKeyRef` env — never inline. `.status` reports the phase, the resolved URL, and per-deployment
resolution.

Contract: [BDR-015](../../design/decision-records/business/studio/015-enterprise-studio.md).
Architecture: [ADR-018](../../design/decision-records/architecture/018-enterprise-studio.md).
Runnable manifests: [`examples/studio/`](../../../examples/studio/README.md).

## Prerequisites

- The operator installed ([install guide](../02-operator-installation/03-install.md)).
- At least one **Enterprise** `Neo4j` in the same namespace to serve.
- An **asset store** — a Neo4j database plus an account with the `architect` role. You either
  pre-create it (BYO) or let the operator provision it (below).

## A minimal Neo4jStudio

One in-cluster deployment and a BYO asset store ([`01-minimal.yaml`](../../../examples/studio/01-minimal.yaml)):

```yaml
apiVersion: neo4j.com/v1
kind: Neo4jStudio
metadata:
  name: studio-minimal
spec:
  deployments:
    - name: my-graph                 # display name in Studio
      neo4jRef:
        name: my-graph               # an in-cluster Neo4j; the operator resolves its URI
                                     # mode defaults to ReverseProxy
  assetStore:
    uri: "neo4j://my-graph.default.svc:7687"
    database: studio-assets          # must already exist; its account needs the architect role
    auth:
      secretRef:
        name: studio-assetstore-auth
```

```bash
kubectl apply -f examples/studio/01-minimal.yaml
kubectl wait --for=condition=StudioReady neo4jstudio/studio-minimal --timeout=300s
kubectl get neo4jstudio studio-minimal -o jsonpath='{.status.url}{"\n"}'
```

`version` defaults to the operator's pinned Studio tag; `license` is optional (Studio starts
unlicensed — a license Secret's key must be `nes.license`).

## Serving a Neo4j

Each entry in `deployments[]` is one connection shown in Studio. It targets an in-cluster
`neo4jRef` (the operator resolves the address) or an external `uri`, in one of two modes:

| Mode | Who terminates the connection | Use when |
|------|-------------------------------|----------|
| `ReverseProxy` (default) | Studio proxies HTTP(S) to the DBMS; the **browser only talks to Studio** | the in-cluster-safe default — the DBMS address need not be browser-reachable |
| `Direct` | the **browser** dials Bolt straight at the DBMS | you need a non-routing path (e.g. cluster GDS), or a browser-reachable endpoint |

`ReverseProxy` against a cluster needs the served Neo4j's **HTTP(S) connector** enabled, and the
operator makes Studio's own front door sticky (`sessionAffinity: ClientIP`) so explicit HTTP
transactions stay pinned to one member (BDR-016).

`Direct` against an in-cluster `neo4jRef` needs a **browser-reachable** address: either an explicit
`deployments[].publicUri`, or — for an exposed analytics/read pool — a derived one (see
[Cluster GDS](#cluster-gds)). Admission rejects `Direct` + `neo4jRef` without a reachable address,
because the in-cluster Service DNS the operator would derive is not reachable from a browser.

## The asset store

Studio needs a database (and an `architect` account) to persist its own assets. Two ways:

- **BYO** — pre-create the database and account, reference them via `assetStore.uri` / `database` /
  `auth.secretRef` (the minimal example above).
- **Operator-provisioned** — point `assetStore.neo4jRef` at an in-cluster Neo4j and set
  `provision: true`. The operator CREATEs the database, the `architect` account, and the grant over
  its admin Bolt session (idempotently), **and generates the auth Secret if it's absent** (user
  `studio` + random password). See [`03-provisioned.yaml`](../../../examples/studio/03-provisioned.yaml):

```yaml
spec:
  assetStore:
    neo4jRef:
      name: my-graph
    provision: true
    database: studio-assets
    auth:
      secretRef:
        name: studio-assetstore-auth   # omit the Secret itself → operator generates it
```

Provisioning needs the target to allow an admin Bolt session (NEO-004): set
`spec.trust.certificates.bolt` (verified TLS, recommended) or `spec.trust.insecureAdminConnection: true`
on that `Neo4j`. While it runs, the CR sits in `Pending` / reason `StudioProvisioning`.

## TLS and trust

An in-cluster Neo4j running TLS presents a certificate from a **private** CA (cert-manager or BYO) —
never a public root. For `neo4jRef` targets the operator resolves that CA and mounts it into the
Studio pod (`SSL_CERT_DIR`), so NES trusts the private CA **on top of** the public roots and verifies
the backing DB fully (`neo4j+s`, not encrypt-but-don't-verify). This is automatic and adds no CR
field. External `uri` deployments bring their own trust.

One boundary no operator can cross: a `Direct` deployment makes the **browser** verify the DBMS cert,
so each end user's browser must trust the CA that signs the Bolt certificate. Solve it by issuer
choice — a corporate CA already on managed devices, or a publicly-trusted cert for the external name.

## Cluster GDS

Graph Data Science lives only on the **analytics** pool, its graph catalog is server-local, and the
analytics members are read-only secondaries. A GDS call therefore needs a **direct, non-routing** Bolt
connection pinned to that pool — routing (`neo4j://`) or the HTTP Query API would land it on a primary
that has no GDS (`50N42 … server-side routing is disabled`). A `ReverseProxy` deployment cannot serve
cluster GDS for that reason.

The supported pattern (mirroring Neo4j's own guidance): expose the analytics pool and serve it with a
`Direct` deployment. Set `secondaries.analytics.service.publicHost` on the `Neo4j` — the operator
renders that pool's **own** external Bolt Service (the north-south client Service fronts only
primaries), adds `publicHost` to the Bolt cert SANs, and a `Direct` deployment targeting
`neo4jRef.pool: analytics` gets a derived, non-routing `bolt+s://<publicHost>:7687` `publicUri`
automatically — no hand-written URI, no trust editing. Add a second `ReverseProxy` deployment at the
primaries for data loads and `gds.*.write*` (the analytics pool is read/compute-only). Full manifest:
[`05-cluster-gds.yaml`](../../../examples/studio/05-cluster-gds.yaml).

```yaml
# Neo4j: expose the analytics pool
spec:
  topology:
    secondaries:
      analytics:
        members: 1
        plugins: [gds]
        service:
          type: LoadBalancer
          loadBalancerSourceRanges: ["203.0.113.0/24"]   # restrict to your clients (ADD-08)
          publicHost: gds.example.com                    # external DNS; added to the Bolt SANs
---
# Neo4jStudio: Direct to the pool + ReverseProxy for write-back
spec:
  deployments:
    - name: GDS (analytics)
      neo4jRef: { name: cl, pool: analytics }
      mode: Direct                                       # publicUri derived: bolt+s://gds.example.com:7687
    - name: cl (ingest / write)
      neo4jRef: { name: cl }                             # primaries, ReverseProxy
```

Two operational prerequisites: point a **DNS record** for `publicHost` at the pool Service's external
address (a cert cannot be issued for a raw IP), and ensure the **queried database is allocated to the
analytics server** (`ALTER DATABASE <db> SET TOPOLOGY … SECONDARIES` if it predates the pool).

## Opening Studio

```bash
kubectl port-forward svc/studio-minimal 8080:8080   # then http(s)://localhost:8080
```

Or expose it with `connectivity.service` (`LoadBalancer`) or an `Ingress` — see
[`04-aks-letsencrypt-tls.yaml`](../../../examples/studio/04-aks-letsencrypt-tls.yaml) for a public
HTTPS setup with cert-manager Let's Encrypt.

## Troubleshooting

The common failures — asset-store `CrashLoopBackOff`, `x509: certificate signed by unknown authority`,
cluster-GDS `no procedure gds.*`, `Direct` resolve errors, stuck `StudioProvisioning` — are catalogued
with fixes in the [examples troubleshooting guide](../../../examples/studio/README.md#troubleshooting).

## Next

[Clustering](02-clustering.md) · [Plugins](07-plugins.md) ·
[Security](05-security.md) · [Error reference](../05-reference/errors.md) ·
[Studio examples](../../../examples/studio/README.md)
