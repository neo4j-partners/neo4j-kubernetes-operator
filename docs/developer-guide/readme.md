# Developer Guide

For people changing the operator. If you only want to run Neo4j on Kubernetes, you want the
[user guide](../user-guide/readme.md) instead.

| Page | Covers |
|------|--------|
| [1. Contributing](01-contributing.md) | Branch and pull request workflow, the CI gates, running them locally, fast-forward merges, commits, reviews |
| [2. Changing the code](02-changing-the-code.md) | What a change has to carry, adding a condition or Event reason, where each package lives |

## Where the rest of the developer material lives

Not everything is in this directory yet. These are the sources to read, and they are maintained where
the thing they describe lives:

| Topic | Where |
|-------|-------|
| End-to-end harness — how to run suites, add a case, configure profiles | [`tests/contribute.md`](../../tests/contribute.md) |
| End-to-end harness design — suites, pipelines, actions, assertions | [`tests/design.md`](../../tests/design.md) |
| What the end-to-end suites actually cover | [`tests/coverage.md`](../../tests/coverage.md) |
| Architecture decisions — pipeline, layering, status, testing strategy | [`docs/design/decision-records/architecture/`](../design/decision-records/architecture/) |
| Product and API decisions — topology, storage, TLS, config surface | [`docs/design/decision-records/business/`](../design/decision-records/business/) |
| Security model — operator privilege, Secret consent | [`docs/design/security.md`](../design/security.md) |
| CRD specification — spec, status, validation rules | [`docs/design/crd-spec/`](../design/crd-spec/) |
| Helm chart values | [`charts/neo4j-operator/README.md`](../../charts/neo4j-operator/README.md) |

## Quick start for a first change

```bash
make test                     # unit tests
make audit                    # CRD validator, reconcile linter, error catalog projections
make errors                   # regenerate the error reference and tests/lib/oracle.sh
make install                  # CRD into your cluster (server-side apply)
make run LOG_ARGS="--zap-devel --zap-log-level=debug"
```

`make run` executes the controller on your machine against your kubeconfig, so you iterate without
building an image. Do not leave the in-cluster Deployment running at the same time.

Then read [Contributing](01-contributing.md) before opening a pull request — the two rules there
(green checks, fast-forward only) determine how your work lands — and
[Changing the code](02-changing-the-code.md) for what the change itself has to carry.
