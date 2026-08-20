# Neo4j Kubernetes Operator — User Guide

Everything you need to install the operator, run Neo4j on Kubernetes, and operate it day to day.

This guide is self-contained: it links only to its own pages and to the runnable manifests under
[`examples/`](../../examples/). You never need to read the operator source or the design records
to use the product.

## Where to start

If you have never run the operator, follow **[Getting started](01-getting-started/readme.md)**.
It takes you from an empty cluster to a Neo4j instance answering queries, and it tells you what
the operator can and cannot do today.

If the operator is already running and you want to shape a deployment — clustering, storage,
TLS, plugins, metrics — go straight to the topic you need in **[Neo4j](03-neo4j/readme.md)**.

## Contents

| Section | What it covers |
|---------|----------------|
| [1. Getting started](01-getting-started/readme.md) | Platform walkthroughs, your first Neo4j, current feature status |
| [2. Operator installation](02-operator-installation/readme.md) | Installing from the published release or from source, watch scope, uninstalling |
| [3. Neo4j](03-neo4j/readme.md) | One page per concern: topology, storage, connectivity, security, configuration, plugins, monitoring, operations |
| [4. Troubleshooting](04-troubleshooting/01-common-issues.md) | Symptom-driven fixes, and how to read the operator log |
| [5. Reference](05-reference/api.md) | Complete `Neo4j` API reference, error catalog, operator-owned settings |

## The two objects you work with

The operator and the databases it manages have separate lifecycles, and this guide is organised
along that split.

You install the **operator** once per cluster: a controller Deployment in its own namespace,
plus the `Neo4j` CustomResourceDefinition. It watches one namespace by default.

You then create one **`Neo4j` custom resource** per database deployment. The operator turns it
into StatefulSets, Services, ConfigMaps, Secrets and PersistentVolumeClaims, keeps them aligned
with the spec, and reports what it did through status conditions and Kubernetes Events.

```yaml
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata:
  name: dev
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

## Conventions used in this guide

Installing the operator needs no clone: the image, the chart and the CRD are published with every
release, as described in [Operator installation](02-operator-installation/readme.md). Commands that
do run from the repository root — anything using `make`, and the manifests under
[`examples/`](../../examples/) — say so. Namespaces are explicit in every `kubectl` command; where a
manifest omits `metadata.namespace`, the resource lands in `default`.

Field paths are written as you would type them in YAML, for example
`spec.storage.volumes.data.mode`. Every field mentioned in a topic page is listed with its type,
default and constraints in the [API reference](05-reference/api.md).
