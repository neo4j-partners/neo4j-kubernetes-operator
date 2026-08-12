# Neo4j Kubernetes Operator

Run Neo4j Enterprise on Kubernetes by declaring one resource. The operator turns a `Neo4j` object into
the StatefulSets, Services, ConfigMaps, Secrets and PersistentVolumeClaims that a working Standalone
instance or a Neo4j cluster needs, then keeps them converged and reports what it sees back in the
resource status.

```text
API:    neo4j.com/v1beta1
Kind:   Neo4j
Module: github.com/neo4j/neo4j-kubernetes-operator
```

The unit of management is the deployment, not the data: the operator owns pods, storage, connectors,
TLS material and configuration. Databases, users, roles and backups are not managed by a resource
today — see [what works today](docs/user-guide/01-getting-started/feature-status.md).

## What a resource looks like

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev-minimal
spec:
  edition: enterprise
  version: "2026.05.0"
  license:
    accept: "yes"
  topology:
    mode: Standalone
  storage:
    volumes:
      data:
        mode: Dynamic
        dynamic:
          size: 10Gi
  auth:
    generatePassword: true
```

That is the whole input. Everything else — connector addresses, discovery, cluster formation settings,
labels, probes — is derived, and the settings the operator owns are
[listed explicitly](docs/user-guide/05-reference/operator-owned-config.md) so nothing is overridden
silently.

## Requirements

- Kubernetes 1.28 or later, with a StorageClass that can provision volumes. Older API servers do not
  evaluate the CRD's validation rules.
- **Neo4j Enterprise.** Community is rejected at admission. `spec.license.accept: "yes"` is a required
  field: it records that you hold a license from Neo4j for the image you are about to run.
- Permission to install a CRD (cluster-scoped) and to create RBAC in the operator namespace.
- The operator watches **one namespace by default**, set through `WATCH_NAMESPACE`. Cluster-wide watch
  is deliberately refused — see [operator scope](docs/user-guide/02-operator-installation/04-operator-scope.md).

There is no public operator image: the Helm chart defaults to an internal registry
(`neo4joperatoracr.azurecr.io`), so installing starts by building and publishing your own.

## Quick start

Starting from nothing, follow a platform guide — [local kind](docs/user-guide/01-getting-started/local-kind.md)
or [Azure AKS](docs/user-guide/01-getting-started/azure-aks.md) — which cover creating the cluster and
making the image reachable. With a cluster and a registry already in place:

```bash
# 1. Build and publish the operator image
export IMG=<registry>/neo4j-kubernetes-operator:dev
make docker-build && docker push "$IMG"

# 2. Install the CRD, RBAC and the controller
make deploy IMG="$IMG"          # or: make helm-install IMG="$IMG"

# 3. Deploy a Standalone instance
kubectl apply -f examples/standalone/01-minimal.yaml
kubectl get neo4j dev-minimal -w
```

`kubectl get neo4j` prints edition, version, topology mode and readiness:

```text
NAME          EDITION      VERSION     MODE         READY   AGE
dev-minimal   enterprise   2026.05.0   Standalone   True    3m
```

Then read the generated password and open a connection:

```bash
kubectl get secret dev-minimal-auth -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d; echo
kubectl port-forward svc/dev-minimal 7687:7687 7474:7474
```

When readiness does not arrive, `kubectl describe neo4j dev-minimal` shows the conditions, and every
`reason` it can report is catalogued in the [error reference](docs/user-guide/05-reference/errors.md).
Start troubleshooting from [common issues](docs/user-guide/04-troubleshooting/01-common-issues.md).

## Feature status

Standalone and Cluster deployment, storage, connectors and Service exposure, TLS, authentication,
configuration and JVM settings, plugins, scheduling, probes and Prometheus metrics are implemented, and
most are verified end-to-end on every pull request. Database and user management, backup and restore,
version upgrades, Ingress, LDAP and multi-cluster are not implemented.

Read [what works today](docs/user-guide/01-getting-started/feature-status.md) before relying on a
field: it is the maintained list, and it separates what is tested end-to-end from what is implemented
but unverified, and from schema fields that are still inert.

## Documentation

| Read this | For |
| --------- | --- |
| [User guide](docs/user-guide/readme.md) | Installing the operator and running Neo4j — the entry point |
| [Getting started](docs/user-guide/01-getting-started/readme.md) | kind and AKS walkthroughs, your first instance |
| [What works today](docs/user-guide/01-getting-started/feature-status.md) | Implementation status, field by field |
| [Neo4j topics](docs/user-guide/03-neo4j/readme.md) | Clustering, storage, connectivity, security, configuration, plugins, monitoring, operations |
| [Troubleshooting](docs/user-guide/04-troubleshooting/01-common-issues.md) | Symptoms, causes, fixes |
| [API reference](docs/user-guide/05-reference/api.md) | Every `spec` and `status` field, with defaults and constraints |
| [Developer guide](docs/developer-guide/readme.md) | Contributing, CI gates, code layout |
| [Design records](docs/design/) | Why the API and the architecture look like this — BDRs, ADRs, CRD spec |

The user guide is self-contained: it links only to its own pages and to `examples/`, so you can read it
without navigating the design tree.

## Examples

[`examples/`](examples/README.md) holds apply-ready manifests — Standalone and Cluster variants,
storage modes, Secret handling, TLS — each with a header stating its purpose and prerequisites. The
end-to-end fixtures under `tests/fixtures/` mirror several of them, so those shapes are exercised on
every run.

A smaller kubebuilder-style sample lives in [`config/samples/`](config/samples/).

## Contributing

Work on a branch, open a pull request against `main`, and expect the checks to gate it: a pull request
with a failing check is not merged, and `main` only moves forward by fast-forward. The full workflow,
the local commands that reproduce CI, and what a change is expected to touch are in
[Contributing](docs/developer-guide/01-contributing.md).

```bash
make test     # unit tests
make audit    # CRD validator + reconcile linter
make run      # controller on your machine, against your kubeconfig
```

## License

This operator is licensed under the [Apache License 2.0](LICENSE). Neo4j Enterprise itself is
separately licensed: running it requires an agreement with Neo4j, which is what
`spec.license.accept` acknowledges.
