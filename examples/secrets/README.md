# Secrets prerequisites

Secrets used across the `examples/` tree: static auth/license Secrets checked in here, and
TLS material generated on demand with `./hack/gen-cluster-tls.sh`.

> **Why do I need these labels?** Helm required nothing like them, because `helm install` used *your*
> credentials — naming a Secret granted you nothing you did not already have. An operator mounts
> Secrets on your behalf with *its* ServiceAccount, which would otherwise turn "may create a Neo4j CR"
> into "may read every Secret in the namespace". Full rationale, threat model and rejected
> alternatives: [Security — why the operator requires opt-in labels](../../docs/user-guide/03-neo4j/05-security.md#why-the-operator-requires-opt-in-labels).

## Mountable Secrets (NEO-005)

Any Secret the operator mounts into Neo4j pods must carry:

```yaml
metadata:
  labels:
    neo4j.com/mountable-by-operator: "true"
```

This includes BYO TLS Secrets, `storage.secretMounts`, `auth.passwordSecretRef`, and plugin
`licenseSecretRef`. Operator-generated auth Secrets (`generatePassword: true`) get the label
automatically. `hack/gen-cluster-tls.sh` labels the TLS Secrets it creates.

`storage.secretMounts` and `trustedCerts.sources` must also list `items` (named keys only).

## BYO auth Secret delegation (ADD-01)

`auth.passwordSecretRef` is stronger than a mount: the operator may read `NEO4J_AUTH` to dial
Bolt (Cluster formation). A BYO auth Secret must also be delegated to **one** Neo4j CR:

```yaml
metadata:
  labels:
    neo4j.com/mountable-by-operator: "true"
    neo4j.com/allowed-for: "<Neo4j.metadata.name>"   # e.g. dev-auth-secret
```

Operator-managed `{name}-auth` Secrets (from `generatePassword: true`) are already labeled
`app.kubernetes.io/managed-by=neo4j-operator` + `app.kubernetes.io/instance=<name>` and need no
`allowed-for`.

`connectivity.clusterDomain` only affects Neo4j-advertised DNS / `CLUSTER_DOMAIN`. The operator
always dials the short in-cluster Service name (`<svc>.<ns>.svc`) and never appends the CR's
`clusterDomain` to that URI.

## Static Secrets

| File | Creates | Used by |
|------|---------|---------|
| `create-auth-secret.sh` | `neo4j-auth` (Opaque, key `NEO4J_AUTH`, random password) | `standalone/02-auth-existing-secret.yaml` |
| `plugin-licenses.yaml` | `gds-license`, `bloom-license` (dummy `license: REPLACE_ME`) | `cluster/03-pools-analytics-read.yaml`, `cluster/14-full.yaml` |

```bash
# NEO-021: do not apply a committed password — generate locally
./examples/secrets/create-auth-secret.sh default dev-auth-secret neo4j-auth
kubectl apply -f examples/secrets/plugin-licenses.yaml
```

# Or equivalent one-liner:
# kubectl create secret generic neo4j-auth \
#   --from-literal="NEO4J_AUTH=neo4j/$(openssl rand -base64 24)" \
#   --dry-run=client -o yaml | \
#   kubectl label --local -f - neo4j.com/mountable-by-operator=true neo4j.com/allowed-for=dev-auth-secret -o yaml | \
#   kubectl apply -f -

Replace the dummy license values with your real GDS/Bloom license file contents before relying
on Enterprise features of those plugins.

## TLS material — `./hack/gen-cluster-tls.sh`

`hack/gen-cluster-tls.sh <namespace> <name> <primary-count>` generates a self-signed lab CA and
one server certificate, then creates three Secrets in the target namespace:

```
<name>-cluster-key   # key: private.key
<name>-cluster-cert  # key: public.crt
<name>-cluster-ca    # key: ca.crt
```

`<name>` must match the `metadata.name` of the `Neo4j` CR that will reference these Secrets — the
script bakes `<name>-primary-<ordinal>.<namespace>.svc.cluster.local` DNS names into the
certificate SAN list for `<primary-count>` primaries.

### Standalone (`dev`)

```bash
./hack/gen-cluster-tls.sh default dev 1
```

Produces `dev-cluster-key` / `dev-cluster-cert` / `dev-cluster-ca` in namespace `default`. Wire
these into `spec.trust.certificates.bolt` and/or `spec.trust.certificates.https` — Standalone has
no `trust.certificates.cluster` (that block is Cluster-only). See
`../standalone/07-tls-https-bolt.yaml` and `../standalone/08-tls-bolt-only.yaml`.

### Cluster (`prod`)

```bash
./hack/gen-cluster-tls.sh default prod 3
```

Produces `prod-cluster-key` / `prod-cluster-cert` / `prod-cluster-ca` in namespace `default`, with
one certificate covering all 3 primary DNS names. Wire these into
`spec.trust.certificates.cluster` (mTLS between members, `clientAuth: Require` +
`trustedCerts.sources` pointing at the `-ca` Secret) and optionally `https` / `bolt`. See
`../cluster/06-tls-full.yaml` and `../cluster/07-tls-cluster-only.yaml`.

Re-run with a different `<primary-count>` argument if you change `topology.primaries.members` —
the SAN list is baked in at generation time.

### EXTRA_DNS (LoadBalancer / Browser HTTPS)

Azure/cloud LoadBalancer IPs are not stable enough to bake into a certificate SAN. If you're
testing Neo4j Browser over HTTPS through a `LoadBalancer` Service, point a stable DNS name at the
LB IP and pass it in:

```bash
EXTRA_DNS=neo4j.example.com ./hack/gen-cluster-tls.sh default prod 3
```

Then create the DNS record (or `/etc/hosts` entry for a lab) pointing `neo4j.example.com` at the
current LoadBalancer IP, and open `https://neo4j.example.com:7473/` — never browse to the bare
LB IP over HTTPS (Jetty SNI will reject it).

### Browser: `bolt+s://` vs `neo4j+s://`

- `bolt+s://host:7687` — direct, unrouted driver connection. Works against a single Standalone
  instance or when connecting straight to one cluster member.
- `neo4j+s://host:7687` — routed connection; the driver discovers cluster topology and routes
  reads/writes to the correct member. Use this against a Cluster's client Service/LoadBalancer.

Both require Bolt TLS (`trust.certificates.bolt`) — Neo4j Browser served over HTTPS refuses to
open a plaintext Bolt connection from a secure page, so `connectivity.listeners.https` always
needs `trust.certificates.bolt` alongside `trust.certificates.https`.
