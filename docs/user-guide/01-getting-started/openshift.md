# Quickstart — OpenShift (ARO, ROSA, self-managed)

Minimal path from zero to a running Standalone Neo4j on OpenShift. This applies to every OpenShift
distribution — self-managed, Red Hat OpenShift on AWS (ROSA), and
[Azure Red Hat OpenShift (ARO)](https://learn.microsoft.com/azure/openshift/) — because what
differs from plain Kubernetes is OpenShift itself, not the cloud underneath it.

On x86/ARM clusters (including ARO) this uses the **published operator and Neo4j images**, so you
need no clone and no registry of your own. IBM Z (`s390x`) is the one exception — those images are
not published for s390x and must be built in-cluster; see
[OpenShift on IBM Z](#openshift-on-ibm-z-s390x) at the end.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| An OpenShift 4.x cluster | 4.21 ships Kubernetes 1.34, which meets the operator floor |
| Kubernetes 1.34 or later | A cluster below 1.34 fails at install with `chart requires kubeVersion: >= 1.34.0-0` |
| [`oc`](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) | Logged in with `cluster-admin` (CRDs and the operator are cluster-scoped installs) |
| [Helm](https://helm.sh/docs/intro/install/) 3.8+ | Installs the chart from its OCI registry |
| A default StorageClass | `oc get storageclass` must show one marked `(default)`, or name one in the CR |
| Neo4j Enterprise image pull | Nodes pull `docker.io/neo4j:<version>-enterprise`; add a pull Secret if egress is restricted |

Shared requirements: [operator prerequisites](../02-operator-installation/01-prerequisites.md).

---

## What is different on OpenShift

Three things set OpenShift apart from a plain Kubernetes install. The operator and its defaults
already handle all three — this section explains *why* so the behavior isn't surprising.

### 1. Security context — the SCC assigns the UID

`platform` defaults to `auto`. The operator detects OpenShift (the `security.openshift.io` API is
present) and **leaves `runAsUser`, `runAsGroup`, and `fsGroup` unset** on the Neo4j pod. The
`restricted-v2` SCC then assigns a UID from the project's allocated range and admits the pod — **no
`anyuid`/`nonroot` SCC grant is required**. On plain Kubernetes the same default instead pins UID,
GID, and `fsGroup` to `7474` (a fresh volume needs it to be writable by a non-root process).

Do not force `platform: kubernetes` on OpenShift — that pins `7474`, which `restricted-v2` rejects
with `FailedCreate … runAsUser: 7474 must be in ranges`. Details:
[Pod and container hardening](../03-neo4j/05-security.md#pod-and-container-hardening) and the
[chart OpenShift notes](../../../charts/neo4j-operator/README.md#openshift).

### 2. Image names must be fully qualified

OpenShift's container runtime (CRI-O) runs **short-name mode in `enforcing`**, so a bare image
reference like `neo4j:<version>` is rejected as ambiguous (`InspectFailed … short name mode is
enforcing`). The operator's default `spec.image.repository` is `docker.io/neo4j` — already fully
qualified — so a CR that sets no repository works out of the box. If you point at your own registry
(an ACR/ECR mirror, or the OpenShift internal registry), always use the fully-qualified host, e.g.
`myregistry.example.com/neo4j`.

### 3. Any non-Docker-Hub registry must be allowlisted

Separately from the runtime, the operator enforces an image allowlist (NEO-012). The default allows
`neo4j`, `docker.io/neo4j`, and `index.docker.io/neo4j`. A CR pointing anywhere else is rejected
(`spec.image.repository … is not in the operator allowlist`) until you add that registry at install:

```bash
--set 'allowedImageRepositories={docker.io/neo4j,myregistry.example.com/neo4j}'
```

If you pull from another project's ImageStream, that project's `default` ServiceAccount also needs
`system:image-puller` on the source namespace.

---

## Install steps

### 1. Install the operator

```bash
oc new-project neo4j-operator-system 2>/dev/null || oc project neo4j-operator-system

VERSION=1.0.0

oc apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system \
  --set 'watchNamespaces={neo4j}' \
  --wait --timeout 300s
```

`platform` is left at `auto` on purpose — it detects OpenShift. Confirm it did:

```bash
oc logs -n neo4j-operator-system deploy/neo4j-operator-controller-manager \
  | grep "resolved operand platform"
# -> "resolved operand platform" platform=openshift
```

### 2. Install Neo4j

```bash
oc new-project neo4j 2>/dev/null || oc project neo4j

oc apply -f - <<'EOF'
apiVersion: neo4j.com/v1
kind: Neo4j
metadata:
  name: dev
  namespace: neo4j
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
          # storageClassName: <name>   # omit to use the cluster default
  auth:
    generatePassword: true
EOF

oc get neo4j dev -n neo4j -w
```

No `spec.image` block is needed — the default `docker.io/neo4j` is fully qualified and allowlisted.

### 3. Verify the security context

This is the step most people second-guess. The **StatefulSet template** shows only what the operator
wrote, which on OpenShift is deliberately just `runAsNonRoot` and the seccomp profile:

```bash
oc get sts dev-server -n neo4j \
  -o jsonpath='{.spec.template.spec.securityContext}{"\n"}'
# {"runAsNonRoot":true,"seccompProfile":{"type":"RuntimeDefault"}}
```

The UID is injected by the SCC at pod-admission time, so it appears on the **Pod**, not the template:

```bash
oc get pod dev-server-0 -n neo4j -o jsonpath='{.metadata.annotations.openshift\.io/scc}{"\n"}'
# restricted-v2
oc get pod dev-server-0 -n neo4j -o jsonpath='{.spec.containers[0].securityContext.runAsUser}{"\n"}'
# e.g. 1000760000 — from the project's uid-range
```

When `oc get neo4j dev -n neo4j -o jsonpath='{.status.phase}'` reads `Running` and
`conditions[Ready]` is `True`, connect per [Your first Neo4j](first-neo4j.md#5-connect).

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Pod never created; STS event `FailedCreate … restricted-v2: Forbidden … runAsUser: 7474 must be in ranges` | Operator started with `platform: kubernetes`, which pins UID 7474 | Leave `platform` at `auto` (chart default), or `--set platform=openshift` |
| `InspectFailed … short name mode is enforcing` / image is ambiguous | A CR set a bare image name (no registry host) | Use a fully-qualified `spec.image.repository`; the default `docker.io/neo4j` is already qualified |
| `spec.image.repository … is not in the operator allowlist` | Custom registry not allowlisted (NEO-012) | Add it via `--set allowedImageRepositories={…}` at install |
| `ImagePullBackOff` pulling from another project's ImageStream | Missing cross-namespace pull permission | Grant `system:image-puller` to the operand ServiceAccount on the source namespace |
| `ImagePullBackOff`, `no image found in image index for architecture "s390x"` | IBM Z node, but the published image is amd64/arm64 only | Build in-cluster — see [OpenShift on IBM Z](#openshift-on-ibm-z-s390x) |

More: [Common issues](../04-troubleshooting/01-common-issues.md).

---

## OpenShift on IBM Z (s390x)

The published images have no `linux/s390x` entry, so on IBM Z you build both the operator and Neo4j
images **in-cluster on the s390x nodes** and install from the OpenShift internal registry. That
complete, validated walkthrough — BuildConfigs, registry, sized example CRs, and the gotchas the live
run surfaced — lives in [`hack/openshift-s390x/README.md`](../../../hack/openshift-s390x/README.md).
It is validation tooling for an architecture Neo4j does not officially support.

---

## Go deeper

| Topic | Page |
|-------|------|
| The resource you just created | [Your first Neo4j](first-neo4j.md) |
| Pod/container security and SCCs | [Security](../03-neo4j/05-security.md#pod-and-container-hardening) |
| Chart values, including `platform` | [Chart README](../../../charts/neo4j-operator/README.md#openshift) |
| Exposing Bolt beyond port-forward | [Connectivity](../03-neo4j/04-connectivity.md#loadbalancer) |
| What is implemented today | [What works today](feature-status.md) |
| Something is wrong | [Troubleshooting](../04-troubleshooting/01-common-issues.md) |
