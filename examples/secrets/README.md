# Secrets prerequisites

Secrets used across the `examples/` tree: static auth/license Secrets checked in here, and
TLS material generated on demand with `./hack/gen-cluster-tls.sh`.

## Static Secrets

| File | Creates | Used by |
|------|---------|---------|
| `auth-password.yaml` | `neo4j-auth` (Opaque, key `NEO4J_AUTH`) | `standalone/02-auth-existing-secret.yaml` |
| `plugin-licenses.yaml` | `gds-license`, `bloom-license` (dummy `license: REPLACE_ME`) | `cluster/03-pools-analytics-read.yaml`, `cluster/14-full.yaml` |

```bash
kubectl apply -f examples/secrets/auth-password.yaml
kubectl apply -f examples/secrets/plugin-licenses.yaml
```

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
