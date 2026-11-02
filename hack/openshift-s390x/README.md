# Validating the operator on OpenShift / IBM Z (s390x)

Neither the operator image nor the Neo4j image is published for `s390x` (both are amd64/arm64
only). But the operator is pure Go and Neo4j is pure Java, so both run on s390x once built for it —
Neo4j itself is proven on IBM Z by [neo4j-partners/neo4j-z-testbed](https://github.com/neo4j-partners/neo4j-z-testbed).
This kit builds both images **in-cluster on the s390x nodes** and pushes them to the OpenShift
**internal registry**, then installs the operator against them.

Everything here is validation tooling for an architecture Neo4j does not officially support, using
self-built, unofficial images. It is not a supported production path.

## How the internal-registry push works (the short answer)

You do **not** run `podman push` or log in to a registry. Each `BuildConfig` in [`builds.yaml`](builds.yaml)
has `output.to.kind: ImageStreamTag`. A Docker-strategy build runs on an s390x node, and when it
finishes buildah pushes the result to the internal registry automatically, authenticated as the
project's `builder` ServiceAccount. The image then lives at a cluster-internal address every node
can pull without egress:

```
image-registry.openshift-image-registry.svc:5000/<project>/<imagestream>:<tag>
```

The build *context* (Dockerfile + files) is uploaded from your laptop with
`oc start-build --from-dir`, so the source never needs to be in git on the cluster side.

> Manual alternative (reference only): you can't build s390x on an amd64 laptop without emulation,
> but to push a pre-built image you would expose the registry route
> (`oc patch configs.imageregistry.operator.openshift.io/cluster --type=merge -p '{"spec":{"defaultRoute":true}}'`),
> then `oc registry login` and `podman push "$(oc registry info)/neo4j/neo4j:2026.09.0"`. The
> BuildConfig flow below avoids all of that.

## Prerequisites

- `oc` logged in as `cluster-admin`, pointed at the s390x cluster (`oc get nodes -o wide` → `s390x`).
- The internal registry enabled: `oc get configs.imageregistry.operator.openshift.io/cluster -o jsonpath='{.spec.managementState}'` → `Managed`. If `Removed`, give it storage first.
- Build-time egress to `docker.io` (base `eclipse-temurin`, the Neo4j file source) and to
  `gcr.io`/`docker.io` for the operator build (golang, distroless). Runtime needs **no** egress —
  pulls are from the internal registry. If you have no `docker.io` egress at build time, see
  [Tarball variant](#tarball-variant).

## Steps

```bash
# 0. Projects
oc new-project neo4j-operator-system 2>/dev/null || oc project neo4j-operator-system
oc new-project neo4j                 2>/dev/null || oc project neo4j

# 1. ImageStreams + BuildConfigs
oc apply -f hack/openshift-s390x/builds.yaml

# 2. Build the Neo4j s390x image (uploads this dir as context; pushes to the internal registry)
oc start-build neo4j-s390x -n neo4j --from-dir=hack/openshift-s390x --follow

# 3. Build the operator s390x image (context = repo root, uses the repo's Dockerfile)
oc start-build neo4j-operator-s390x -n neo4j-operator-system --from-dir=. --follow

# 4. Confirm both tags landed in the registry
oc get istag -n neo4j neo4j:2026.09.0-enterprise
oc get istag -n neo4j-operator-system neo4j-operator:1.0.0

# 5. CRDs. platform auto (any OpenShift, not only s390x) leaves the Neo4j UID unset —
#    see ../../docs/user-guide/03-neo4j/05-security.md#pod-and-container-hardening
oc apply --server-side --force-conflicts \
  -f config/crd/bases/neo4j.com_neo4js.yaml \
  -f config/crd/bases/neo4j.com_neo4jbackups.yaml \
  -f config/crd/bases/neo4j.com_neo4jbackupschedules.yaml \
  -f config/crd/bases/neo4j.com_neo4jrestores.yaml

# 6. Operator, pointed at the internal-registry image.
#    Do not pass --set image.digest. The published and ACR indexes are amd64/arm64 only;
#    that digest is what produces "no image found in image index for architecture s390x".
helm upgrade --install neo4j-operator ./charts/neo4j-operator \
  --namespace neo4j-operator-system \
  -f hack/openshift-s390x/values-s390x.yaml
oc get pods -n neo4j-operator-system -w

# 7. Deploy Neo4j. platform defaults to auto, so on OpenShift the operator does not pin
#    UID 7474 and restricted-v2 admits the pod. No SCC grant.
oc apply -f hack/openshift-s390x/neo4j-s390x.yaml
oc get neo4j dev -n neo4j -w
```

When `oc get neo4j dev -n neo4j -o jsonpath='{.status.phase}'` reads `Running`, connect per
[Your first Neo4j](../../docs/user-guide/01-getting-started/first-neo4j.md#5-connect).

## Deploy a 3-member cluster

Same images, same registry — only the CR changes ([`neo4j-cluster-s390x.yaml`](neo4j-cluster-s390x.yaml)).

```bash
oc apply -f hack/openshift-s390x/neo4j-cluster-s390x.yaml
oc get neo4j cl -n neo4j -w          # wait for READY=True / ClusterFormed=True
```

The operator proves formation itself over Bolt (`ClusterFormed=True (Formed)` → "All desired servers
enabled"); you do not need to run anything inside the pods — and on a tight-memory cluster you must
not (see below).

## Gotchas the live validation surfaced

| Symptom | Cause | Fix |
|---------|-------|-----|
| `spec.image.repository "neo4j" is not in the operator allowlist` | CR omitted `spec.image.repository`, so it defaulted to `neo4j`, which isn't allowlisted | Set `spec.image.repository` to the internal-registry path on **every** CR (standalone and cluster) |
| Exit 1, `AccessDeniedException: /var/lib/neo4j/metrics` | Image owned by group `7474`; OpenShift runs as an arbitrary UID in group `0` | Rebuild `Dockerfile.neo4j` (`chown 7474:0` and `chmod g=u`) and roll the pods with `pullPolicy: Always` |
| Pod never created, STS event `FailedCreate … restricted-v2: Forbidden … runAsUser 7474 must be in ranges` | Operator was started with `platform: kubernetes`, which pins UID 7474 | Leave `platform` at `auto` (the chart default). It detects OpenShift and does not pin a UID, so `restricted-v2` admits the pod |
| `ErrImagePull … manifest unknown` for `…/neo4j:<v>-enterprise` | `spec.version` points at a tag you never built | Set `spec.version` to a version whose `…-enterprise` tag exists in the ImageStream (`oc get istag -n neo4j`) |
| Pod `Pending`, `Insufficient memory` / `cpu` | Default per-member request is `2Gi` / `500m`; these workers have ~1Gi free each | Set small `spec.resources` + a small heap/page cache via `spec.config.neo4j` (see the cluster CR) |
| A member container restarts with `exit code 137` right after you `oc exec … cypher-shell` | On a tight memory limit the extra cypher-shell JVM OOM-kills the container | Don't run cypher-shell inside a capped pod — connect over `oc port-forward` from your laptop |

## Rebuilds

Re-run the relevant `oc start-build … --follow`. `pullPolicy: Always` (set in
[`values-s390x.yaml`](values-s390x.yaml) and [`neo4j-s390x.yaml`](neo4j-s390x.yaml)) makes pods pick
up the new image on restart; `oc rollout restart` the operator Deployment or the Neo4j StatefulSet
to force it.

## Tarball variant

If the cluster cannot reach `docker.io` at build time, replace the `COPY --from=neo4j-src …` lines
in [`Dockerfile.neo4j`](Dockerfile.neo4j) with an unpack of the official unix tarball you upload in
this directory, and drop the `neo4j-src` stage:

```dockerfile
# (download neo4j-enterprise-2026.09.0-unix.tar.gz from the Neo4j Deployment Center into this dir)
COPY neo4j-enterprise-2026.09.0-unix.tar.gz /tmp/neo4j.tgz
RUN mkdir -p /var/lib/neo4j && tar -xzf /tmp/neo4j.tgz -C /var/lib/neo4j --strip-components=1 && rm /tmp/neo4j.tgz
```

The tarball ships the product but **not** the container `/startup` scripts. Fetch those once from a
machine with egress and commit them next to the Dockerfile:

```bash
oc image extract docker.io/library/neo4j@sha256:11782dc24a4156700e04e3cefe84b5a766eef994b733d560e7841c30c66ee51b \
  --path /startup:./startup --confirm
```

then `COPY startup /startup`. The base `eclipse-temurin:21-jre` must also be mirrored into a
reachable registry.
