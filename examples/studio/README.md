# Enterprise Studio — quick start

Stand up **Neo4j Enterprise Studio** declaratively with a `Neo4jStudio` CR and point it at one or
more `Neo4j` deployments. Contract: [BDR-015](../../docs/design/decision-records/business/studio/015-enterprise-studio.md).
Architecture: [ADR-018](../../docs/design/decision-records/architecture/018-enterprise-studio.md).

The operator reconciles a `Neo4jStudio` into a stateless `Deployment` (image `neo4j/enterprise-studio`)
plus a `Service` and optional `Ingress`. All settings are rendered as `NES_*` environment variables
(the documented approach — they override the image's bundled `config.yaml`); credentials ride as
`secretKeyRef` env and never appear inline. `.status` reports the phase, resolved URL and per-deployment
resolution.

## What Studio needs (BDR-015)

| Dependency | Provided by | Notes |
|------------|-------------|-------|
| **Served deployment(s)** | `deployments[]` — `neo4jRef` (in-cluster) or external `uri` | one-to-many; each `Direct` or `ReverseProxy` (§2) |
| **Asset store** | a Neo4j database + an account with the `architect` role | **you pre-create it** (BYO — §4-a); the operator does not create it |
| **License** (optional) | `license.secretRef` | distinct from the Neo4j license; Studio starts without it (§3) |
| **Sign-in / roles** (optional) | `deployments[].auth` (basic/oidc toggles), `deployments[].roleMapping` | Studio uses the end-user's own login — no per-deployment service credentials |
| **HTTPS** (optional) | `server.tls` → a `kubernetes.io/tls` Secret | serves Studio over HTTPS on its own port |
| **Tools** (optional) | `tools` (dashboards/bloom/query toggles) | all enabled by default |
| **Any other NES setting** | `config` (non-secret) / `configFromSecrets` (secret-valued) | verbatim `NES_*` passthrough — covers genAI, OIDC, mapTiles, … |

`ReverseProxy` against a cluster additionally needs the served Neo4j's **HTTP(S) connector** enabled
(§6); the operator makes Studio's own front door sticky when any served deployment is ReverseProxy
against a cluster (§7).

## Prerequisites

- A running cluster with `kubectl`, and the operator installed
  ([install guide](../../docs/user-guide/02-operator-installation/03-install.md)).
- A `Neo4j` **Enterprise** deployment named `my-graph` in your namespace (any example under
  [`../standalone/`](../standalone/) or [`../cluster/`](../cluster/) works; `02-full.yaml` uses
  `ReverseProxy`, which needs the HTTP connector — see [`../standalone/07-tls-https-bolt.yaml`](../standalone/07-tls-https-bolt.yaml)).
- A pre-created **asset database** (`studio-assets`) whose account has the `architect` role.

All commands target your current namespace; add `-n <ns>` to use another.

## 1. Pre-create the asset database (BYO — §4-a)

```bash
PW=$(kubectl get secret my-graph-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d | cut -d/ -f2)
kubectl exec my-graph-server-0 -c neo4j -- \
  cypher-shell -u neo4j -p "$PW" "CREATE DATABASE \`studio-assets\` IF NOT EXISTS;"
# Create (or reuse) an account with the architect role for Studio to use — e.g.:
kubectl exec my-graph-server-0 -c neo4j -- cypher-shell -u neo4j -p "$PW" \
  "CREATE USER studio SET PLAINTEXT PASSWORD 'change-me' CHANGE NOT REQUIRED; GRANT ROLE architect TO studio;"
```

## 2. Create the referenced Secrets

Studio references every credential by `secretRef` — nothing is inline (§5). Studio does **not** store
per-deployment service credentials: end users sign in with their own Neo4j login (basic or OIDC, toggled
via `deployments[].auth`). The only credential the minimal example needs is the asset-store account:

```bash
# Asset-store credentials (the architect account from step 1):
kubectl create secret generic studio-assetstore-auth \
  --from-literal=username=studio --from-literal=password='change-me'
```

`02-full.yaml` is **self-contained** — it bundles the Secrets it references (`studio-assetstore-auth`,
`studio-tls`, `studio-genai`, and an optional commented `studio-license`) as leading documents, so you
can skip the manual `kubectl create` above for it. Replace every `REPLACE_ME` in that file before any real use.

If you supply a license, the license Secret's key **must be `nes.license`** — the operator mounts it and
points `NES_license_path` at it. Studio starts without a license (§3).

## 3. Apply a Neo4jStudio

```bash
kubectl apply -f examples/studio/02-full.yaml          # self-contained; or 01-minimal.yaml
kubectl get neo4jstudio studio-full                    # VERSION / PHASE / URL columns

# Wait for Studio to be Ready and read its URL:
kubectl wait --for=condition=StudioReady neo4jstudio/studio-full --timeout=300s
kubectl get neo4jstudio studio-full -o jsonpath='{.status.url}{"\n"}'

# Per-deployment resolution:
kubectl get neo4jstudio studio-full \
  -o jsonpath='{range .status.deployments[*]}{.name}{" -> "}{.resolvedUri}{"\n"}{end}'
```

## 4. Open Studio

```bash
kubectl port-forward svc/studio-full 8080:8080
# then browse https://localhost:8080 — 02-full.yaml enables server.tls (self-signed: accept the warning).
# Without server.tls it's plain http://localhost:8080. Or expose via connectivity.service: LoadBalancer / an Ingress.
```

## Examples

| File | Demonstrates |
|------|--------------|
| [`01-minimal.yaml`](01-minimal.yaml) | Smallest valid `Neo4jStudio` — one in-cluster `ReverseProxy` deployment + BYO asset store |
| [`02-full.yaml`](02-full.yaml) | **Self-contained, runnable** — bundles the Secrets, then an in-cluster `ReverseProxy` deployment, BYO asset store, sign-in toggles, role mapping, HTTPS (`server.tls`), tool toggles, replicas, Service, and both `config` / `configFromSecrets` passthroughs |

## Troubleshooting

- **Pod `CrashLoopBackOff`** — Studio validates its asset store **at startup and exits if it can't
  connect**. Confirm the `assetStore.uri` is reachable from the pod, the `database` exists, and the
  account in `studio-assetstore-auth` has the `architect` role (step 1). Check `kubectl logs`.
- **`container has runAsNonRoot and image will run as root`** — you're on an older render; the operator
  now runs Studio as non-root UID `7474`. Rebuild/redeploy the operator.
- **No license** — Studio logs `running unlicensed` and still starts (BDR-015 §3); add a license Secret
  (key `nes.license`) to remove the warning.
- **`Transaction … not found` / `neo4j-cluster-affinity` when serving a cluster in `ReverseProxy`** — HTTP
  explicit transactions must stay pinned to one cluster member. The operator sets `sessionAffinity: ClientIP`
  on a cluster `Neo4j`'s client Service by default (BDR-016), so this works out of the box. If you overrode
  it with `spec.connectivity.service.sessionAffinity: None`, set it back to `ClientIP` (or omit it).

## Cleanup

```bash
kubectl delete -f examples/studio/01-minimal.yaml --ignore-not-found
kubectl delete secret studio-assetstore-auth --ignore-not-found
# the asset database is yours (BYO) — drop it if you created it only for this:
# kubectl exec my-graph-server-0 -c neo4j -- cypher-shell -u neo4j -p "$PW" "DROP DATABASE \`studio-assets\` IF EXISTS;"
```
