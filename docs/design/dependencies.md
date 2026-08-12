# External dependencies and platform constraints

Prerequisites outside the operator codebase: inter-CRD ordering, cluster add-ons, and **cloud- or platform-specific constraints** that affect Neo4j deployment and validation.

**Status**: `[~]` partial — platform matrix below is a first draft; operator-internal dependency chain (Neo4j → day-2 CRDs) to be added.

**Related**: `15-test-strategy.md` (which platform runs which test tier), `16-security.md` (PSS / SCC / RBAC), `03-variant_matrix.csv` (Cloud Storage, LoadBalancer variants), `EST-TST-020` (CI platform matrix).

---

## Platform constraint matrix (draft)

Directional comparison for **Local**, **Azure (AKS)**, **Google (GKE)**, **Amazon (EKS)**, and **OpenShift**.  
Cells list typical mechanisms — not an exhaustive support matrix. Confirm against target cluster version before V1 scope lock (`13-v1-scope-lock.md`).

| Domain | Local (kind / k3s / minikube) | Azure (AKS) | Google (GKE) | Amazon (EKS) | OpenShift (OCP / ROSA / ARO) |
|--------|-------------------------------|-------------|--------------|--------------|------------------------------|
| **Identity** | Default `ServiceAccount`; no cloud IAM. Optional static credentials in Secrets for dev. | **Workload Identity** (Azure AD federated credential): UAMI ↔ KSA. Legacy: aad-pod-identity (deprecated). | **Workload Identity**: GSA ↔ KSA binding (`iam.gke.io/gcp-service-account` annotation). | **IRSA** (IAM Roles for Service Accounts) via OIDC provider. **EKS Pod Identity** (newer alternative). | **SCC**-bound `ServiceAccount`; cloud IAM via platform-specific operators (ROSA/ARO IRSA/WI). No single model across OCP flavors. |
| **Storage** | `local-path`, `hostPath`, or default SC; no zone redundancy. Manual PV for some scenarios. | **Azure Disk CSI** / **Azure Files CSI** — `managed-csi`, `managed-csi-premium`, ZRS disks. Volume expansion supported on Disk. | **GCE PD CSI** — `premium-rwo`, `standard-rwo`; **regional PD** for multi-zone; volume expansion; optional **Hyperdisk**. | **EBS CSI** — `gp3`, `io2`; **EFS CSI** for RWX; volume expansion; zone-aware PVCs. | Platform CSI (EBS/Azure Disk/GCE PD on IPI) or **ODF** / **Ceph** / **Portworx**. StorageClass names vary by platform install. |
| **Network** | `NodePort` / `port-forward`; **MetalLB** optional for LB simulation. No cloud LB. Basic **NetworkPolicy** if CNI supports it. | **Azure Load Balancer** (public / internal) via Service `type: LoadBalancer`; annotations for internal LB, pip, subnets. **Azure CNI** / overlay; **NetworkPolicy** (Azure NPM or Cilium). | **GCE / GLB** via Service annotations (`cloud.google.com/l4-rilb`, etc.); internal LB; **NetworkPolicy** (Calico / Cilium). **GKE Dataplane V2** optional. | **AWS Load Balancer Controller** (NLB/ALB); SG for Pods; **NetworkPolicy** (VPC CNI). Classic ELB annotations legacy. | **OpenShift Route** / **Ingress Controller** (HAProxy) often preferred over raw `LoadBalancer`. **NetworkPolicy** supported (OVNKubernetes / SDN). Cloud LB still available on ROSA/ARO. |
| **Security** | PSS often **disabled** or permissive; no cloud policy engine. Suitable for dev only. | **Pod Security Standards** (baseline / restricted) enforced via Azure Policy or admission. AKS **Defender** optional. | **PSS** (baseline / restricted); **Autopilot** enforces stricter defaults (resource limits, capability drops, restricted volumes). | **PSS** via admission; **IRSA** least-privilege for workloads. Security groups at node and (with VPC CNI) pod level. | **Security Context Constraints (SCC)** — primary model (`restricted-v2`, `nonroot`, etc.). **Compliance Operator** / catalog profiles on OCP. Not PSS-native — mapping required. |
| **Availability** | Single-node or multi-node kind; **no real AZ spread**. PDB optional; no node auto-upgrade. | **PDB**; **node pools** with **availability zones**; cluster autoscaler; surge upgrades (maintenance windows). | **PDB**; **node auto-upgrade** / auto-repair; **topology spread constraints** across zones; regional clusters. | **PDB**; **multi-AZ** node groups; **Karpenter** or cluster autoscaler; managed node upgrade workflows. | **PDB**; **MachineSets** / **MachineConfigPools**; zone spread on cloud IPI; controlled upgrades via OCP lifecycle. |
| **Observability** | Prometheus / Grafana optional (helm); no managed metrics. Log via `kubectl logs` / sidecar. | **Azure Monitor** / **Container Insights**; **Azure Managed Prometheus**; AMW integration. | **Google Cloud Managed Service for Prometheus**; **Cloud Logging** / Cloud Monitoring; **Managed Grafana** optional. | **Amazon Managed Prometheus**; **CloudWatch Container Insights**; ADOT collector optional. | **Cluster Monitoring Operator**; **User Workload Monitoring** (Thanos); platform-integrated metrics on ROSA/ARO. |
| **Cloud backup** | **PVC / local path** only; no object store IAM. S3/GCS/Azure simulators rare in kind. | **Azure Blob** + Workload Identity / SAS; aligns with `Cloud Storage / With Workload Identity` variant. | **GCS** + Workload Identity; uniform bucket-level access; aligns with Helm `analytics` / backup-to-cloud patterns. | **S3** + IRSA; bucket policies; optional **AWS Backup** integration. | **OADP** / **Velero** + object storage (S3/Azure/GCS depending on platform); no single Neo4j-native standard on OCP. |
| **Certificates** | **Self-signed** or **cert-manager** (manual CA); no DNS integration in kind without mock. | **cert-manager** + **Azure DNS** DNS01; **Key Vault CSI** or **Secret Store CSI** for TLS material. | **cert-manager** + **Cloud DNS** DNS01; **Certificate Authority Service (CAS)** for private CA. | **cert-manager** + **Route 53** DNS01; **ACM** for public certs (often at LB, not pod). | **cert-manager** (OperatorHub / OLM); **Ingress cert** via router; cluster **custom CA**; **cert-manager Operator** on OCP common. |

---

## Notes for the operator design

### Portable vs platform-specific

| Portable (operator core) | Platform-specific (variants / samples / docs) |
|--------------------------|-----------------------------------------------|
| CRD spec, reconcile, status | LB Service annotations |
| StatefulSet, PVC, probes | StorageClass names, CSI driver presence |
| Backup/restore job logic | Cloud object store + IAM/WI/IRSA wiring |
| TLS mount paths in Neo4j pod | cert-manager issuer + DNS zone per cloud |

The operator should **not** hard-code cloud annotations in reconcile logic — expose them via CRD (`spec.connectivity`, `spec.persistence`, `spec.trust`) and document per-platform values in `samples/` and migration guides (`EST-DOC-002`).

### V1 platform targeting (proposal — to lock in `13-v1-scope-lock.md`)

| Tier | Platform | Role |
|------|----------|------|
| **P0 dev / CI** | Local (kind) | Default pyramid — unit, integration, most E2E |
| **P0 cloud smoke** | Azure (AKS) | First real-cloud validation of WI/IRSA, CSI, LB, PDB |
| **P1** | Remaining clouds in matrix | Same harness, delta doc only (`EST-TST-020`) |
| **P2** | GKE Autopilot, OpenShift SCC hardening | Stricter admission; often separate test fixtures |
