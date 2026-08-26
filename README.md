# Neo4j Kubernetes Operator

Run Neo4j Enterprise on Kubernetes by declaring one resource. The operator turns a `Neo4j` object into
the StatefulSets, Services, ConfigMaps, Secrets and PersistentVolumeClaims that a working Standalone
instance or a Neo4j cluster needs, then keeps them converged and reports what it sees back in the
resource status.

## Requirements

- Kubernetes 1.28 or later, with a StorageClass that can provision volumes. Older API servers do not
  evaluate the CRD's validation rules.
- **Neo4j Enterprise or Community.** Enterprise requires `spec.license.accept: "yes"`, which records
  that you hold a license from Neo4j for the image you are about to run. Community needs no licence
  and runs Standalone only — clustering, backup and metrics are Enterprise capabilities.
- Permission to install a CRD and to create RBAC in the operator namespace.

## Quick start

On a Kubernetes cluster you already have, installing means applying the CRD and installing the chart — nothing is built.
`VERSION` is the tag of the [latest release](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases) without its leading `v`:

```bash
VERSION=1.0.0-rc1

# 1. The CRD
kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

# 2. The controller, in its own namespace, watching default
helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace --wait

# 3. A Standalone instance, from this repository
kubectl apply -f examples/standalone/01-minimal.yaml
kubectl get neo4j dev-minimal -w
```

If you are starting further back, or want to run your own build:

- [Quickstart — local kind](docs/user-guide/01-getting-started/local-kind.md) — create a local cluster, then the install above
- [Quickstart — Azure AKS](docs/user-guide/01-getting-started/azure-aks.md) — subscription setup, resource providers, and the AKS storage class
- [Quickstart — Google Kubernetes Engine](docs/user-guide/01-getting-started/gcp-gke.md) — project setup, the GKE auth plugin, and the GKE storage class
- [Quickstart — Amazon EKS](docs/user-guide/01-getting-started/aws-eks.md) — cluster creation with eksctl, the EBS CSI driver, and a gp3 storage class
- [Operator installation](docs/user-guide/02-operator-installation/readme.md) — the install paths compared, chart values, watch scope

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
but unverified, from what is planned with a settled design, and from what is not decided yet.

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

## License

This operator is licensed under the [Apache License 2.0](LICENSE). Neo4j Enterprise itself is
separately licensed: running it requires an agreement with Neo4j, which is what
`spec.license.accept` acknowledges.
