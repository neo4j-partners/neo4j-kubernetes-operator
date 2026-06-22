# 5 — Technical plan: Azure adapter (AKS) — V1 target cloud

Implements the `Platform` interface from [4-technical-shared.md](4-technical-shared.md) for **AKS**. This is the **only** Azure-specific code; everything else is shared. Package: `internal/platform/azure`.

## What "Azure" means concretely
| `Platform` method | Azure implementation |
|---|---|
| `DefaultStorageClass` | `managed-csi` / `managed-csi-premium` (Azure Disk CSI driver `disk.csi.azure.com`). Premium SSD for HA data. |
| `SupportsVolumeExpansion` | Azure Disk CSI storage class with `allowVolumeExpansion: true` → online expand (V1-014/041). |
| `BuildExternalExposure` | `Service type=LoadBalancer` + AKS annotations: internal vs public LB (`service.beta.kubernetes.io/azure-load-balancer-internal`), resource group, health-probe path. NLB-equivalent = Azure Standard LB (V1-032). |
| `ConfigureWorkloadIdentity` | **Azure AD Workload Identity**: annotate ServiceAccount (`azure.workload.identity/client-id`), label pod (`azure.workload.identity/use: "true"`), project the federated token. No static secrets (V1-040/042). |
| `PodSecurityDefaults` | restricted PSS baseline (non-root, seccomp `RuntimeDefault`, drop caps) — portable, no AKS exception needed. |
| `DetectCapabilities` | probe for Prometheus Operator CRDs, ingress controller (AGIC or ingress-nginx), list `*.csi.azure.com` storage classes, identity = `workload-identity` (V1-043). |

## Azure building blocks (some seeded from the reference `azure/` manifests we saw)
- **Identity:** Azure AD Workload Identity (federated credential on a user-assigned Managed Identity → AKS OIDC issuer). Used later for backup-to-Blob auth (V2); in V1 it's the identity story for the operator/pods (V1-040/042).
- **Storage:** Azure Disk CSI (`managed-csi-premium`), `allowVolumeExpansion: true`, zone-redundant scheduling for HA (V1-026 cross-zone).
- **Networking:** Azure Standard Load Balancer for `clientService` (internal by default, public opt-in); optional ingress via AGIC/ingress-nginx for Bolt sticky sessions (V1-034).
- **(V2) Secrets/CSI:** Azure Key Vault via Secrets Store CSI driver — out of V1 (V1 TLS = user-supplied secret).

## Azure-specific design notes
- **Zones:** AKS availability zones drive `topologySpreadConstraints` for V1-026 (cross-zone) and AZ-failure hardening; the *constraint logic* is shared, only the zone topology key is standard (`topology.kubernetes.io/zone`).
- **LoadBalancer health probes:** Azure LB needs an explicit health-probe annotation pointing at Neo4j's HTTP/Bolt readiness — set in `BuildExternalExposure`.
- **Internal vs public:** default internal LB (safer for a database); public requires explicit opt-in in `clientService.annotations` + `loadBalancerSourceRanges`.

## Validation on Azure (the V1 deliverable)
Real **AKS** account + CI credentials must land in **week 1** (ring-fenced). E2E covers the Azure rows + Azure-resolved shared rows:
- Install/operate (V1-001..005), standalone+config (G2), cluster/HA on AKS (G3), Azure LB + ingress (G4), TLS user-cert (G5), **Azure identity/disk/capability-detect (G6: V1-040..044)**, metrics (G7), day-2 + decommission (G8), then the 23 hardening items (AZ failure, disk latency, etc.) on real AKS.
- Output: **evidence bundles** per group (manifests applied, status reached, timings).

## Definition of done (Azure / V1)
All 57 V1 items green on real AKS, evidence bundles captured, the `generic`-adapter contract test still green (proves no Azure logic leaked into the core), and the AWS adapter is *implementable* purely by writing `internal/platform/aws` — no core changes required.
