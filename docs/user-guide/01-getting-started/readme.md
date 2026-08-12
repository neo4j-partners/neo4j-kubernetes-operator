# Getting started

Three ways in, depending on what you already have.

| You have | Start here | Time |
|----------|-----------|------|
| Nothing — a laptop with Docker | [kind (local)](local-kind.md) | ~15 min |
| An Azure subscription | [Azure AKS](azure-aks.md) | ~30 min |
| A running cluster with the operator installed | [Your first Neo4j](first-neo4j.md) | ~5 min |

Before you invest time, check [what works today](feature-status.md). The `Neo4j` CRD is
deliberately wider than the current implementation, so a field can exist in the schema and be
accepted at admission without the operator acting on it. That page lists exactly which parts are
implemented, which are inert, and which are planned.

## What you get

Both platform walkthroughs end at the same place: the operator running in
`neo4j-operator-system`, and one Standalone Neo4j instance in `default` with a generated
password, reachable over Bolt and HTTP.

From there, [Your first Neo4j](first-neo4j.md) explains the resource you just created — what the
operator built, how to read its status, how to connect — and points at the topic pages for
anything you want to change.

## Next steps

| Goal | Page |
|------|------|
| Understand the CR you deployed | [Your first Neo4j](first-neo4j.md) |
| Install the operator on another cluster | [Operator installation](../02-operator-installation/01-prerequisites.md) |
| Move from one instance to a cluster | [Clustering](../03-neo4j/02-clustering.md) |
| Something is wrong | [Troubleshooting](../04-troubleshooting/01-common-issues.md) |
