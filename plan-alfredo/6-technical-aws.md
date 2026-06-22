# 6 — Technical plan: AWS adapter (EKS) — follow-on

The AWS adapter is the **proof that the shared design works**: it should be deliverable by writing **only** `internal/platform/aws` and re-running the existing suite — **zero changes to the shared core or the CRD**. If AWS forces a core change, the seam was wrong (and the V1-044 contract test should have caught it).

Implements the same `Platform` interface from [4-technical-shared.md](4-technical-shared.md), for **EKS**. Package: `internal/platform/aws`.

## What "AWS" means concretely
| `Platform` method | AWS implementation |
|---|---|
| `DefaultStorageClass` | `gp3` (EBS CSI driver `ebs.csi.aws.com`); io2 option for high-IOPS HA data. |
| `SupportsVolumeExpansion` | EBS CSI storage class with `allowVolumeExpansion: true` → online expand. |
| `BuildExternalExposure` | `Service type=LoadBalancer` + **AWS Load Balancer Controller** annotations (`service.beta.kubernetes.io/aws-load-balancer-type: external/nlb`, scheme internal vs internet-facing, target-type, health-check). |
| `ConfigureWorkloadIdentity` | **IRSA** (IAM Roles for Service Accounts): annotate ServiceAccount (`eks.amazonaws.com/role-arn`); pod assumes the role via the OIDC provider. No static keys. |
| `PodSecurityDefaults` | same restricted PSS baseline as Azure (portable). |
| `DetectCapabilities` | probe Prometheus Operator CRDs, AWS LB Controller / ingress, list `*.ebs.csi.aws.com` storage classes, identity = `irsa`. |

## Mapping Azure ↔ AWS (everything else is identical)
| Concern | Azure (V1) | AWS (follow-on) |
|---|---|---|
| Block storage | Azure Disk CSI `managed-csi-premium` | EBS CSI `gp3`/`io2` |
| External LB | Azure Standard LB | NLB via AWS LB Controller |
| Pod identity | AAD Workload Identity | IRSA |
| Zones | `topology.kubernetes.io/zone` (AKS AZs) | `topology.kubernetes.io/zone` (EKS AZs) |
| Reconcile core / CRD / quorum / upgrade / config / decommission | **shared — unchanged** | **shared — unchanged** |

The standard zone topology key means cross-zone scheduling (V1-026) is literally the same code on both clouds.

## Effort
Because only the adapter is new, AWS ≈ **the adapter implementation + one real-EKS E2E pass** (mirrors the customer table's AWS rows `T0001–T0076` + `GAP-008` IRSA). The estimate maps to ~one adapter's worth of work, not a second operator.

## Validation on AWS
Re-run the **same** group suite (G1–G8 + hardening) on real **EKS**:
- IRSA in place of Workload Identity (incl. `GAP-008` — IRSA service-account annotation, no static key).
- EBS storage, NLB exposure, EKS AZ spread.
- Produce the AWS evidence bundles.

## Definition of done (AWS)
Full suite green on EKS, the contract test still green, and a side-by-side Azure/AWS evidence comparison showing identical behavior from the shared core — confirming the "max shared code" goal.

## Not in this phase
GCP (GKE) and OpenShift adapters follow the same pattern later; they're deferred (see [3-backlog.md](3-backlog.md) coverage).
