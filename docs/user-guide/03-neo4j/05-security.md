# Security

Credentials, transport encryption, and how the pods are hardened. The one part that surprises people
is the label the operator requires on Secrets, so that is explained rather than just stated.

## Authentication

The simplest option is to let the operator generate the initial password:

```yaml
spec:
  auth:
    generatePassword: true
```

It creates `<name>-auth`, a Secret with a single `NEO4J_AUTH` key holding `neo4j/<password>`, labels
it so it can be mounted, and records the name in status. The password is never written to the
resource, to logs or to events:

```bash
kubectl get neo4j dev -n default -o jsonpath='{.status.credentials.secretName}{"\n"}'
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

This is a bootstrap credential, not a managed one. Rotating it afterwards is a Neo4j operation —
`ALTER CURRENT USER SET PASSWORD` — and editing the Secret later does not change the password inside
a database that has already been initialised.

### Bring your own password

Provide the Secret yourself when the password comes from a vault, or must be identical across
environments:

```yaml
spec:
  auth:
    passwordSecretRef:
      name: neo4j-credentials
```

The Secret needs the `NEO4J_AUTH` key in `user/password` form, and two labels:

```bash
kubectl create secret generic neo4j-credentials -n default \
  --from-literal=NEO4J_AUTH='neo4j/a-strong-password'

kubectl label secret neo4j-credentials -n default \
  neo4j.com/mountable-by-operator=true \
  neo4j.com/allowed-for=dev
```

The Neo4j image sets the initial password through a command line, so a few values cannot work:
the password must not start with `-` nor contain `/`, the user part must be `neo4j`, and the
password cannot be `neo4j`. The operator checks this before creating anything and reports
`AuthSecretInvalid` rather than letting the pod crash-loop — see
[Errors](../05-reference/errors.md#authsecretinvalid). Generated passwords are alphanumeric and
never hit these rules.

`generatePassword: true` and `passwordSecretRef` are mutually exclusive, and admission says so.
Example: [`examples/standalone/02-auth-existing-secret.yaml`](../../../examples/standalone/02-auth-existing-secret.yaml),
with more detail in [`examples/secrets/README.md`](../../../examples/secrets/README.md).

`spec.auth.ldap` exists in the schema and does nothing yet.

## Why the operator requires opt-in labels

Every Secret the operator mounts must carry `neo4j.com/mountable-by-operator: "true"` — secret
mounts, TLS material, plugin licences, and bring-your-own passwords. Auth Secrets additionally need
`neo4j.com/allowed-for: <resource name>`. If you came from the Helm chart, which asked for nothing of
the sort, that looks like gratuitous friction. It is not, and the reason is worth two minutes.

**With Helm, you create the pod.** The chart renders a pod spec that mounts a Secret, and the API
server checks whether *you* may create that pod. In Kubernetes, being able to create a pod in a
namespace is already equivalent to being able to read every Secret in it — you control the pod spec,
so you can mount anything and read it. Naming a Secret in Helm values granted you nothing you did not
already have. No third party acted on your behalf, so no consent mechanism was needed.

**With an operator, someone else creates the pod.** You write a `Neo4j` resource, and the operator —
a separate identity, with its own permissions — mounts and reads the Secrets you named. That turns a
narrow permission into a broad one: *being allowed to write a `Neo4j` resource becomes, in effect,
being allowed to read any Secret in the namespace.* Someone deliberately denied pod creation gets
back exactly the capability that denial was meant to withhold.

The labels close that gap. They move the decision from whoever writes the resource to whoever owns
the Secret, and they shrink what a `Neo4j` author can reach from "every Secret in the namespace" to
"the Secrets someone explicitly offered".

So the labels do not make the operator safer than Helm. They restore a property Helm got for free.
If you want the historical parallel: Helm 2's Tiller had the same problem, and the fix was to delete
the server-side component entirely. An operator cannot do that — being the server-side component is
the point — so it rebuilds the property with consent instead. Note also that Helm driven by Argo CD
or Flux reintroduces a privileged controller with no equivalent control, so against Helm plus GitOps
these labels are a net gain rather than a restoration.

### When the label actually buys you something

Be honest about the conditions, because they determine whether this is a real boundary or a
formality:

| Your situation | What the label is worth |
|----------------|------------------------|
| The people writing `Neo4j` resources can also patch Secrets in that namespace | Nothing. They can label any Secret themselves. It is ceremony. |
| They hold only `Neo4j` write permission, and a platform team owns the Secrets | A genuine capability boundary — the mechanism that makes least-privilege delegation expressible at all |

The generated-password path is labelled automatically, so the default experience asks nothing of you.

### The second label

`neo4j.com/allowed-for` is required on bring-your-own auth Secrets specifically, because an auth
Secret is not only mounted into a pod: the operator **reads it** to open an administrative Bolt
session — cluster formation needs one. That is a borrowed credential rather than a mounted file, so
the grant is narrowed from "any `Neo4j` resource in this namespace" to one named resource.

It closes a chain where you reference another instance's auth Secret and steer the operator's
administrative session at that instance. The second half of that chain is already blocked
independently, since the operator always dials the short in-cluster Service name and ignores
`connectivity.clusterDomain` when doing so, which makes this label defence in depth rather than the
only guard. Within a single namespace Kubernetes gives you no tenancy boundary anyway — co-tenants can
read each other's Secrets and exec into each other's pods directly.

### What the labels do not protect

They are not confidentiality against the operator. Kubernetes RBAC cannot filter by label, so the
operator's permissions cover every Secret in the namespaces it watches, and it reads a Secret before
it evaluates the label. At the moment it refuses one, it has already seen the contents.

The property is therefore: the operator will not **propagate** an unlabelled Secret into a pod you
control, and will not **use** it as a credential. It is not: the operator **cannot** read it. If the
operator is compromised or its token leaks, every Secret in the watched namespaces is exposed
regardless of labels. Keep the watch scope narrow — see
[Watch scope and RBAC](../02-operator-installation/04-operator-scope.md).

### What you see when a label is missing

The check needs to read the Secret, so it cannot be a schema rule; it runs during reconcile, and
optionally at admission when the validating webhook is enabled, which it is not by default. With the
webhook off, the resource is accepted and then refused — nothing is deployed, and the refusal is
reported twice:

```bash
kubectl get neo4j dev -n default \
  -o jsonpath='{range .status.conditions[?(@.type=="Error")]}{.reason}: {.message}{"\n"}{end}'

kubectl describe neo4j dev -n default | tail -20
```

The reason is `SecretNotMountable` when the mountable label is missing, `SecretNotDelegated` when the
delegation label is missing or names another resource, and the same identifier appears on a Warning
Event. Both are catalogued in the [error reference](../05-reference/errors.md). No StatefulSet is
created in either case, which is the actual security property.

## Transport security

Neo4j traffic is unencrypted unless you configure TLS. You supply the material; there is no
self-signed fallback, deliberately, because a certificate nobody validates is worse than none.

```yaml
spec:
  trust:
    enabled: true
    certificates:
      bolt:
        privateKey:
          secretName: neo4j-bolt-tls
          subPath: tls.key
        publicCertificate:
          secretName: neo4j-bolt-tls
          subPath: tls.crt
      https:
        privateKey:
          secretName: neo4j-https-tls
          subPath: tls.key
        publicCertificate:
          secretName: neo4j-https-tls
          subPath: tls.crt
```

Those Secrets need the mountable label, like every Secret the operator mounts.

Three policies exist, one per traffic type: `bolt` for driver connections, `https` for the HTTP
listener, and `cluster` for member-to-member traffic. Which ones you must set depends on the
topology, and admission enforces it:

| Topology | With `trust.enabled: true` |
|----------|---------------------------|
| Standalone | At least one of `bolt` or `https`; `cluster` is rejected |
| Cluster | `cluster` is **required**; `bolt` and `https` as needed |

Encrypting client traffic while leaving replication in the clear would be a false sense of security,
which is why a cluster cannot opt out of the `cluster` policy.

Serving HTTPS also needs the listener enabled — see [Connectivity](04-connectivity.md#listeners).

### The operator's own admin session

In Cluster mode the operator drives membership over an authenticated Bolt session as `neo4j`, so it
needs to know how it may open one. Give Bolt certificates as above, or state explicitly that the
session may run unencrypted:

```yaml
spec:
  trust:
    insecureAdminConnection: true
```

A Cluster declaring neither is rejected at admission, since the operator could form nothing. The flag
opens no port and weakens no protocol — without `bolt` TLS the connector is already in the clear for
every client — it only records that you accept the operator's password travelling that way, and it
raises a Warning event each time the session is opened. In Standalone mode the operator opens no admin
session, so the field does nothing. See
[Clustering](02-clustering.md#the-operator-needs-an-admin-session).

### Client certificates

Require or accept client certificates, and supply the CA bundle used to validate them:

```yaml
spec:
  trust:
    enabled: true
    certificates:
      bolt:
        clientAuth: Require
        trustedCerts:
          sources:
            - secret:
                name: client-ca
                items:
                  - key: ca.crt
                    path: ca.crt
```

`clientAuth` is `None`, `Optional` or `Require`. Both `Optional` and `Require` need a non-empty
`trustedCerts.sources`: `Optional` still validates any certificate a client presents, so without a CA
bundle every presented certificate is rejected. Trusted certificate sources are projected volume
sources, so they must name their items explicitly, and Secrets among them need the mountable label.
Sources that would expose ambient cluster identity — service account tokens, the downward API,
cluster trust bundles — are rejected.

### Certificate renewal

```yaml
spec:
  trust:
    reload:
      enabled: true
```

This turns on Neo4j's own TLS reload (`dbms.security.tls_reload_enabled`), so Neo4j picks up changed
certificate files without a restart.

There is a Kubernetes-level caveat you need to plan around. The key and certificate are mounted with
`subPath`, because the data key inside your Secret can be named anything while Neo4j requires the
files at `private.key` and `public.crt`. Kubernetes does not propagate Secret updates into `subPath`
mounts, so **editing a certificate Secret in place does not reach the running pod** and there is
nothing for the reload to pick up. This matches the Helm chart's behaviour, which mounts the same way.

In practice that means:

- **Trusted CA bundles do propagate.** `trustedCerts.sources` are projected per item, so adding or
  rotating a CA reaches the pod and the reload applies it.
- **Renewing a leaf certificate rolls the pods.** The operator hashes the mounted key and certificate
  bytes onto the StatefulSet (`neo4j.com/tls-checksum`) and watches those Secrets, so a cert-manager
  reissue or an in-place Secret edit starts a rolling restart. `kubectl rollout restart` also works —
  the operator keeps `kubectl.kubernetes.io/restartedAt` instead of overwriting it on the next
  reconcile.

### Certificates issued by cert-manager

Instead of supplying key and certificate Secrets yourself, let the operator provision them. Set
`certManager.enabled` and give each policy a target `secretName` only — no `privateKey` or
`publicCertificate`:

```yaml
spec:
  trust:
    enabled: true
    reload:
      enabled: true
    certManager:
      enabled: true
      issuerRef:
        name: corp-ca
        kind: ClusterIssuer     # or Issuer
      includeIngressHosts: true # merge ingress hosts into bolt/https SANs
      dnsNames:
        - bolt.prod.example.com # extra SANs, e.g. an external load balancer
    certificates:
      bolt:
        secretName: prod-bolt-tls
      cluster:
        secretName: prod-cluster-tls
```

The operator creates one `cert-manager.io/v1` `Certificate` per configured policy, named
`{cr-name}-{policy}-tls` and owned by the Neo4j resource, so it is garbage collected with the CR.
cert-manager writes `tls.crt` and `tls.key` into your `secretName`, and the operator mounts them at
the paths Neo4j expects. This needs cert-manager installed in the cluster: if the `Certificate` CRD
is absent the reconcile fails rather than silently doing nothing.

The certificate SANs are derived, not guessed. Every policy gets the in-cluster DNS names Neo4j
actually advertises — the client Service for Bolt and HTTPS, the per-member internals Services for
cluster traffic — enumerated per member rather than covered by a namespace wildcard. `dnsNames` and
ingress hosts are added to the bolt and https certificates only; the cluster certificate stays on
member discovery names. Scaling the deployment updates the `Certificate` and cert-manager reissues.

While issuance is in flight, `TLSReady` reports `CertificatePending` and the operator requeues.

A cert-manager policy and a bring-your-own policy are mutually exclusive: setting `secretName`
alongside `privateKey` or `publicCertificate` is rejected.

Examples: [`examples/standalone/07-tls-https-bolt.yaml`](../../../examples/standalone/07-tls-https-bolt.yaml),
[`examples/cluster/08-tls-cert-manager.yaml`](../../../examples/cluster/08-tls-cert-manager.yaml),
[`examples/cluster/06-tls-full.yaml`](../../../examples/cluster/06-tls-full.yaml),
[`examples/cluster/07-tls-cluster-only.yaml`](../../../examples/cluster/07-tls-cluster-only.yaml).

## Pod and container hardening

The Neo4j container runs as uid and gid 7474 with a matching fsGroup, so volumes are writable without
root. Override the contexts when your cluster's policies demand something specific:

```yaml
spec:
  security:
    podSecurityContext:
      runAsNonRoot: true
      fsGroup: 7474
      seccompProfile:
        type: RuntimeDefault
    containerSecurityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: false
      capabilities:
        drop: [ALL]
```

These are the standard Kubernetes types, passed through as given. `readOnlyRootFilesystem: true` is
not something Neo4j tolerates without extra mounts, so treat it as an experiment rather than a
default.

### ServiceAccount

```yaml
spec:
  security:
    serviceAccount:
      create: true
      annotations:
        example.com/team: platform
```

The workload gets its own ServiceAccount, named after the resource. Cloud workload-identity
annotations — the ones that bind a pod to an IAM role on EKS, GKE or AKS — are rejected: they would
let a `Neo4j` author mint cloud credentials for the database pod, which is a much larger grant than
"run a database".

### NetworkPolicy

```yaml
spec:
  security:
    networkPolicy:
      enabled: true
      # Required when enabled (NEO-010) — client ports never use an empty From.
      ingressFrom:
        - podSelector: {}   # any pod in this namespace; tighten with matchLabels
      # Optional narrower peers (otherwise ingressFrom is reused):
      # backupFrom: [...]
      # metricsFrom: [...]
```

Off by default. When enabled, `ingressFrom` is required so Bolt/HTTP/HTTPS are not
reachable from every pod in the cluster. Cluster-internal ports stay same-namespace only.
Egress is not managed here — rely on your CNI / platform policies. A policy is inert without
a NetworkPolicy-enforcing CNI.

Example: [`examples/standalone/22-security.yaml`](../../../examples/standalone/22-security.yaml).

## Next

[Connectivity](04-connectivity.md) · [Operations](09-operations.md) ·
[Error reference](../05-reference/errors.md)
