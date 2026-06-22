# 4 — Technical plan: shared logic (cloud-agnostic core)

This is the **~85–90% of the codebase that is identical on every cloud**. Azure and AWS differ only behind one interface (the `Platform` seam). The rule: **no cloud-specific `if` ever enters this layer** — anything cloud-specific becomes a `Platform` method. The provider-seam contract test (V1-044 / NEW-004) fails the build if that rule is broken.

## Stack
- **Go**, **Kubebuilder** scaffolding on **controller-runtime** (see decision: official upstream standard).
- **API group** `neo4j.com/v1beta1`. Validation via **CEL** (`x-kubernetes-validations`) — **no webhooks**.
- Tests: `envtest` (API-level), `kind` (integration), real-cloud E2E (per-adapter, in the cloud docs).

## Repository layout
```
api/v1beta1/                 # CRD types (shared)        ── Neo4j, Neo4jBackup, Neo4jRestore (V2)
internal/controller/         # reconcilers (shared)      ── one per CRD, small, no god objects
internal/resources/          # k8s object builders (shared) ── StatefulSet, Services, ConfigMap, Secret, NetworkPolicy
internal/neo4j/              # engine client (shared)    ── formation check, quorum, Cypher seed, admin
internal/platform/           # the SEAM (interface)      ── Platform interface + capability detection
  platform/generic/          #   generic/dev adapter (kind, test fallback)
  platform/azure/            #   → see 5-technical-azure.md
  platform/aws/              #   → see 6-technical-aws.md
config/                      # CRDs, RBAC, samples, manifests
```

## The CRD (shared) — single `Neo4j`
One workload CRD covers standalone (`servers: 1`) and HA (`servers: ≥3` odd). `Neo4jBackup` / `Neo4jRestore` types exist but are **V2**.

Config surface **reuses the Neo4j Helm chart vocabulary** (`neo4jConfig` passthrough, `resources`, `scheduling`, `storageSize`) wrapped in typed, CEL-validated fields + a raw passthrough escape hatch. **Cloud-dependent fields resolve through `Platform`**, never hard-coded.

**Key spec fields**
| Field | Default | Notes |
|---|---|---|
| `version` (req) | — | Neo4j Enterprise tag; downgrades / >1-release jumps blocked by upgrade preflight |
| `servers` | `1` | `1` = standalone; `≥3` odd = HA; **`2` rejected** |
| `acceptLicenseAgreement` (req) | — | must equal `"yes"` |
| `storageSize` | `10Gi` | can grow, never shrink |
| `storageClassName` | nil → `Platform.DefaultStorageClass` | cloud-resolved |
| `resources` | — | memory request ≥ 1.5Gi recommended |
| `neo4jConfig` | — | rendered to `neo4j.conf`; deprecated 4.x keys rejected |
| `tls` | disabled | `{enabled, secretName}`; secret needs `tls.crt`/`tls.key` (PKCS#8)/`ca.crt` |
| `auth` | generate | BYO admin Secret or operator-generated |
| `scheduling` | — | nodeSelector/affinity/tolerations/topologySpread/... |
| `clientService` | `ClusterIP` | `LoadBalancer` → `Platform.BuildExternalExposure` |
| `networkPolicy` | `Generated` | mirror cluster ports + Prometheus |
| `monitoring` | false | metrics + PodMonitor when Prometheus present |

**CEL rules (no webhooks):** `servers==1||servers>=3` · odd count when `>1` · `acceptLicenseAgreement=="yes"` · `tls.enabled ⇒ tls.secretName` · name length ≤ 56. Unsafe scale-in / unsafe upgrade are controller-level guards.

**Status:** `phase` (Pending/Provisioning/Running/Upgrading/Scaling/Degraded/Failed) · `observedGeneration` · `readyServers` · `conditions` (Ready/Progressing/Degraded/Upgradeable/TLSValid) · `endpoints` (bolt/http for client discovery).

> Open API question for week 1: confirm flat `servers` vs `topology.{mode,cores,readReplicas}` — reserve the field names now to avoid a future breaking change.

## The `Platform` seam — the ONLY place clouds differ
```go
// internal/platform/platform.go
type Platform interface {
    Name() string                                              // "azure" | "aws" | "generic"

    // Storage
    DefaultStorageClass(ctx) (string, error)                  // managed-disk CSI vs EBS CSI
    SupportsVolumeExpansion(ctx, sc string) (bool, error)     // V1-014/041

    // Networking / exposure
    BuildExternalExposure(svc *core.Service, spec ClientServiceSpec) error  // LB annotations (V1-032)

    // Identity (no static secrets)
    ConfigureWorkloadIdentity(sa *core.ServiceAccount, pod *core.PodSpec) error  // V1-040/042

    // Pod hardening (SCC-strict by default; OpenShift later)
    PodSecurityDefaults() *core.SecurityContext

    // Runtime capability detection — "detect, don't configure" (V1-043)
    DetectCapabilities(ctx) (Capabilities, error)
}

type Capabilities struct {
    HasPrometheusOperator bool   // PodMonitor vs scrape annotations
    HasIngressController  bool   // optional ingress (V1-034)
    StorageClasses        []string
    IdentityMode          string // "workload-identity" | "irsa" | "none"
}
```
A new cloud = **implement this interface once** + one real-cloud E2E run. No core changes.

## Reconcile core (shared) — `Neo4jReconciler`
Pipeline (from the CRD draft), all cloud-agnostic, calling `Platform` only at the marked steps:
```
fetch → validate(CEL already applied) → resolveStorageClass(Platform*) → ensureSecrets
 → reconcileTLS → reconcileConfigMap(render neo4j.conf, NEW-001) → reconcileServices(Platform*)
 → reconcileNetworkPolicy → reconcileStatefulSet(Platform* securityContext, NEW-002)
 → verifyFormation(internal/neo4j) → collectDiagnostics → updateStatus
deletion: finalizer → quorum-safe member removal → cascade cleanup (NEW-005)
```

### Shared algorithms (the hard, cloud-independent parts)
- **Cluster formation & quorum** (V1-021..027): form 3/5-node, verify via `internal/neo4j`, status `readyServers`.
- **Quorum-safe scaling** (V1-023/024): never remove a member that breaks quorum; controller-level guard + CEL size rules (V1-030).
- **Safe rolling upgrade ordering** (V1-029): secondaries → non-leader primaries → leader last; preflight that **blocks store-migration upgrades** (V1-031).
- **Config rendering** (NEW-001/002): spec → `neo4j.conf` ConfigMap + pod resources/scheduling/securityContext; change triggers safe reconcile.
- **Decommission** (NEW-005): finalizer-driven cascade; remove members in quorum-safe order; reclaim PVC/Service/LB; no orphans.
- **Status model** (V1-010/047/049): conditions (`Ready/Progressing/Degraded/Upgradeable/TLSValid`), `observedGeneration`, failure attribution (cert vs quorum).

## Validation (shared, CEL — no webhooks)
CEL rules live in the CRD (`servers==1||servers>=3`, odd count, license, tls pairing, name length…). Confirm in week 1 that CEL fully covers V1-017/018/030; if a rule can't be expressed in CEL, that's the only thing that would reopen the webhook decision.

## Observability (shared)
- controller-runtime metrics + Neo4j custom metrics; `PodMonitor` when `Capabilities.HasPrometheusOperator`, else scrape annotations (V1-045/046/048).
- Conditions + Events as the human-facing signal (V1-049).

## RBAC / security (shared)
- Least-privilege ClusterRole: **no wildcard/escalate/impersonate** (V1-006), namespaced leader-election Role (V1-007). Static-scanned in CI.
- Pod securityContext from `Platform.PodSecurityDefaults()` (SCC-strict baseline).

## Testing (shared)
- **Unit + envtest** for reconcile logic and CEL rules — cloud-free.
- **`generic` adapter** runs the full reconcile suite on `kind` → this *is* the provider-seam contract test (V1-044): if core needs anything beyond the `Platform` interface, this fails.
- Per-cloud E2E lives in [5-technical-azure.md](5-technical-azure.md) / [6-technical-aws.md](6-technical-aws.md).

## What is NOT here (lives in the adapters)
Storage class names, LB annotations, identity wiring, capability probing specifics, cloud E2E. Everything else is shared.
