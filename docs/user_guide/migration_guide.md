# Upgrade Guide

**v1.13.0 is the first public release of this independent project.** There is no
upgrade path from earlier version numbers — install v1.13.0 fresh (see the
[Installation guide](installation.md)).

> This is a personally maintained, community project with no affiliation to
> Neo4j, Inc. APIs and behaviour **may change between releases** — review the
> release notes before upgrading, and validate independently before relying on
> a new version.

## Upgrading between future releases

When a newer version ships:

1. **Refresh the CRDs first.** Helm does not upgrade CRDs automatically, so a new
   field or validation won't take effect until the CRDs are applied. Apply the
   bundle for the version you are upgrading to (replace the tag):

   ```bash
   kubectl apply --server-side -f \
     https://github.com/priyolahiri/neo4j-kubernetes-operator/releases/download/v1.13.0/neo4j-kubernetes-operator.yaml
   ```

2. **Upgrade the operator** via Helm:

   ```bash
   helm repo update
   helm upgrade neo4j-operator neo4j-operator/neo4j-operator \
     --namespace neo4j-operator-system
   ```

   (or re-apply the complete bundle if you installed with plain `kubectl` —
   see the [Installation guide](installation.md)).

3. **Read the release notes** for that version for any breaking changes or
   manual steps, and roll forward one minor version at a time if you are
   skipping several.

The operator reconciles declaratively, so once the new version is running it
converges existing `Neo4j*` resources to the new behaviour automatically.
