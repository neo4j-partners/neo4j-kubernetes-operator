# Quickstart — Amazon EKS

Minimal path from zero to a running Standalone Neo4j on [Amazon Elastic Kubernetes Service (EKS)](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html).

Everything here uses the published operator image and Helm chart, so you need no clone, no Docker
and no container registry of your own. If you do need to build the operator — you changed it, or
your policy requires images from your own ECR — the two extra steps live in
[Build the operator image](../02-operator-installation/02-build-image.md) and slot in before
step 3, without changing anything else on this page.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| AWS account | Permission to create an EKS cluster, a VPC and IAM roles |
| [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) v2 | Signed in — `aws configure sso` then `aws sso login`, or `aws configure` |
| [eksctl](https://eksctl.io/installation/) | Creates the cluster, its VPC and its IAM roles in one command |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Context set by `eksctl create cluster` |
| [Helm](https://helm.sh/docs/intro/install/) 3.8+ | Installs the chart from its OCI registry |
| Neo4j Enterprise image | EKS nodes must pull `neo4j:<version>` — configure pull Secret if required |

Shared requirements: [operator prerequisites](../02-operator-installation/01-prerequisites.md).

Nothing to enable per account, unlike Azure resource providers or GCP service APIs. Confirm which
account and identity you are about to spend money in, then name the cluster:

```bash
aws sts get-caller-identity

export CLUSTER=neo4j-operator-eks
export REGION=eu-west-1
export ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
```

The IAM part is worth a word before you start. `eksctl` creates a cluster role, a node role and an
OIDC provider on your behalf, so it needs IAM write access — more than the `PowerUserAccess`
policy grants, for instance. In an account where IAM is reserved to a platform team, ask them to
create the cluster, or to grant you those permissions; the rest of this page is unaffected.

---

## Install steps

### 1. Create the EKS cluster

```bash
eksctl create cluster \
  --name "$CLUSTER" \
  --region "$REGION" \
  --nodes 2 \
  --node-type m5.xlarge \
  --with-oidc
kubectl get nodes
```

Expect 15 to 20 minutes: `eksctl` builds a VPC, the control plane and a managed node group, and
writes your kubeconfig at the end. `--with-oidc` is what makes step 2 possible — it is not the
default, and adding it afterwards means another pass.

### 2. Give the cluster a working StorageClass

EKS installs **no CSI driver**, and `kubectl get storageclass` showing `gp2` right after step 1 is
misleading: that class provisions nothing until the driver is in place, so a PersistentVolumeClaim
would sit in `Pending` and the Neo4j pod would never schedule. This is the one step that has no
equivalent on AKS or GKE, where the driver ships with the cluster.

Give the driver an identity, then install it as a managed addon:

```bash
eksctl create iamserviceaccount \
  --cluster "$CLUSTER" --region "$REGION" \
  --namespace kube-system --name ebs-csi-controller-sa \
  --role-name "AmazonEKS_EBS_CSI_DriverRole-${CLUSTER}" \
  --attach-policy-arn arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy \
  --role-only --approve

eksctl create addon --cluster "$CLUSTER" --region "$REGION" --name aws-ebs-csi-driver \
  --service-account-role-arn "arn:aws:iam::${ACCOUNT_ID}:role/AmazonEKS_EBS_CSI_DriverRole-${CLUSTER}" \
  --force
```

Then declare a `gp3` class. EKS ships only `gp2`, served through in-tree CSI migration; naming the
provisioner explicitly removes that indirection, and gp3 is both cheaper and faster:

```bash
kubectl apply -f - <<'EOF'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp3
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
EOF

kubectl get storageclass
```

`WaitForFirstConsumer` binds the volume in the availability zone where the pod lands, which is what
you want: an EBS volume cannot be attached across zones.

### 3. Install the operator

The CRD ships as a release asset and the controller as a chart. Take the version from the
[latest release](https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases) without its
leading `v`:

```bash
VERSION=1.0.0-rc1

kubectl apply --server-side --force-conflicts \
  -f https://github.com/neo4j-partners/neo4j-kubernetes-operator/releases/download/v${VERSION}/neo4j-crd-${VERSION}.yaml

helm upgrade --install neo4j-operator \
  oci://ghcr.io/neo4j-partners/charts/neo4j-operator --version ${VERSION} \
  --namespace neo4j-operator-system --create-namespace \
  --wait --timeout 300s
```

Verify the CRD and controller:

```bash
kubectl get crd neo4js.neo4j.com
kubectl get pods -n neo4j-operator-system
```

Two things this leaves at their defaults: the operator reconciles the `default` namespace only, and
the image comes from GHCR. To widen the scope, or to run your own image from an ECR repository, see
[Watch scope and RBAC](../02-operator-installation/04-operator-scope.md) and
[Point the install at your image](../02-operator-installation/02-build-image.md#point-the-install-at-your-image).

### 4. Install Neo4j

Deploy a Standalone `Neo4j` resource backed by an EBS volume. The manifest is explained field by
field in [Your first Neo4j](first-neo4j.md).

**4a. Apply the CR** (no `metadata.namespace` — deploys to **`default`**)

```bash
kubectl apply -f - <<'EOF'
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
          storageClassName: gp3
  auth:
    generatePassword: true
EOF
```

Alternative — start from [`examples/standalone/01-minimal.yaml`](../../../examples/standalone/01-minimal.yaml)
and add `storageClassName: gp3` under `spec.storage.volumes.data.dynamic`.

**4b. Watch progress**

```bash
kubectl get neo4j dev -n default -w
kubectl get pods -n default -l app.kubernetes.io/instance=dev
```

Expected resources:

| Resource | Name |
|----------|------|
| StatefulSet | `dev-server` |
| Headless Service | `dev-server` |
| Client Service | `dev` |
| Auth Secret | `dev-auth` (operator-generated) |
| ConfigMap | `dev-config` |
| PVC | `data-dev-server-0` |

The PVC stays `Pending` until the pod is scheduled, by design of `WaitForFirstConsumer`. A PVC that
stays `Pending` *after* the pod is scheduled is the symptom of a missing or unhealthy EBS CSI
driver — check `kubectl get pods -n kube-system -l app=ebs-csi-controller` and revisit step 2.

**4c. Check status**

```bash
kubectl get neo4j dev -n default -o wide
kubectl get neo4j dev -n default -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

When ready:

- `status.phase`: `Running`
- `status.conditions[Ready]`: `True`
- `status.credentials.secretName`: `dev-auth`

If the Neo4j pod fails to pull the Enterprise image, create an image pull Secret and set
`spec.image.pullSecrets` on the resource — see
[Operations](../03-neo4j/09-operations.md#pulling-images-from-a-private-registry).

How to read those conditions, and what to do when one is `False`:
[Your first Neo4j](first-neo4j.md#4-read-the-status).

### 5. Connect

Retrieve credentials:

```bash
kubectl get secret dev-auth -n default -o jsonpath='{.data.NEO4J_AUTH}' | base64 -d && echo
```

Port-forward Bolt:

```bash
kubectl port-forward -n default svc/dev 7687:7687
```

Use `neo4j://localhost:7687` with user `neo4j` and the password from the Secret.

Browser HTTP (optional):

```bash
kubectl port-forward -n default svc/dev 7474:7474
# Open http://localhost:7474
```

Connection details, including in-cluster URIs: [Your first Neo4j](first-neo4j.md#5-connect).
To expose Bolt through an AWS load balancer instead of port-forwarding, see
[Connectivity](../03-neo4j/04-connectivity.md#loadbalancer).

---

## Tear down

Delete the resource and its claim before the cluster. An EBS volume is released when its
PersistentVolumeClaim goes, and deleting the cluster first leaves the volume behind, still billing:

```bash
kubectl delete neo4j dev -n default --ignore-not-found
kubectl delete pvc -n default -l app.kubernetes.io/instance=dev

eksctl delete cluster --name "$CLUSTER" --region "$REGION"
```

Then confirm nothing outlived it — an unattached volume costs money for as long as it exists:

```bash
aws ec2 describe-volumes --region "$REGION" \
  --filters Name=status,Values=available \
  --query 'Volumes[].[VolumeId,Size,CreateTime]' --output table
```

`eksctl delete cluster` removes the node group, the control plane and the VPC it created, but not
the IAM role from step 2. Drop it once you are done with the cluster for good:

```bash
aws iam delete-role --role-name "AmazonEKS_EBS_CSI_DriverRole-${CLUSTER}"
```

To remove Neo4j but keep the cluster, note that PersistentVolumeClaims are preserved on purpose —
which is why the teardown above deletes them explicitly. See
[Uninstall](../02-operator-installation/05-uninstall.md#persistentvolumeclaim-retention).

---

## Go deeper

| Topic | Page |
|-------|------|
| The resource you just created | [Your first Neo4j](first-neo4j.md) |
| What is implemented today | [What works today](feature-status.md) |
| Running your own build, from an ECR | [Build the operator image](../02-operator-installation/02-build-image.md) |
| Reconciling namespaces other than `default` | [Watch scope and RBAC](../02-operator-installation/04-operator-scope.md) |
| Shaping the deployment — storage, connectivity, security, plugins | [Neo4j topics](../03-neo4j/readme.md) |
| Uninstalling the operator | [Uninstall](../02-operator-installation/05-uninstall.md) |
| Something is wrong | [Troubleshooting](../04-troubleshooting/01-common-issues.md) |
