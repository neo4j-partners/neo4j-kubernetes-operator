# ADR-016 — Cloud identity for backup and restore

| | |
|---|---|
| **Status** | proposed |
| **Date** | 2026-08-28 |
| **Depends on** | [ADR-015](015-backup-and-restore.md) — backup/restore execution (delegates "credentials / cloud identity" here) · [BDR-014](../business/backup-restore/014-backup-restore.md) — CRD contract (`credentials` fields) · [ADR-013](013-neo4j-conf-directory-fragments.md) — neo4j.conf fragments (connector config) · [ADR-001](001-crd-validation-process.md) — CEL vs webhook · [BDR-005](../business/neo4j/005-storage-volume-mode.md) — storage volumes |
| **Constraints** | No cloud SDK in the operator image (ADR-015) · Enterprise only · workload-identity paths are **not** locally testable (need cloud CI) · V2 |

---

## Context

[ADR-015](015-backup-and-restore.md) fixed the execution model — backup runs in a **Job pod** (`neo4j-admin database backup`), restore runs on the **Neo4j server pods** (online seed-from-URI over Bolt) — and explicitly deferred one thing: *how those pods authenticate to cloud object storage* (S3 / GCS / Azure Blob). This ADR decides that, and only that.

**What already exists (the seams):**

- Backup Job projects `spec.destination.credentials.secretName` as `envFrom` into the Job container ([ADR-015](015-backup-and-restore.md); `render/backup/job.go`). Static-key backup to a cloud bucket already works.
- The workload StatefulSet runs under a **per-instance ServiceAccount** (`OperandServiceAccountName` = the CR name), and `OperandServiceAccount` already copies `spec.security.serviceAccount.annotations` onto that SA.
- A cloud-workload-identity annotation detector (`isCloudWorkloadIdentityAnnotation`: `eks.amazonaws.com/*`, `iam.gke.io/*`, `azure.workload.identity/*`) already exists — but today it is used to **reject** those annotations (NEO-002).

**The two real gaps:**

1. **Backup Job pods have no ServiceAccount** — they fall back to the namespace `default` SA. There is nothing to bind cloud IAM to and no projected OIDC token, so the "omit `credentials` → workload identity" path promised by [BDR-014](../business/backup-restore/014-backup-restore.md) is unimplemented for backup.
2. **The Neo4j workload has no cloud-credential surface at all.** WI annotations on the workload SA are rejected (NEO-002); there is no pod-annotation/label, `serviceAccountName`, or `envFrom`/`extraEnv` field on the workload container. So **cloud restore is blocked**: the seed providers on the server pods cannot authenticate. Azure WI in particular needs a **pod label** (`azure.workload.identity/use: "true"`) plus a projected service-account token — neither has a spec home today.

**What this ADR is *not* about — the credential-free path.** Restore can seed from a backup on the server's own filesystem with **no cloud credentials** via the `file:` (`FileSeedProvider`, 5.26+) and `server:` (`ServerSeedProvider`, 2026.04+) providers — `CREATE OR REPLACE DATABASE db OPTIONS { seedURI:'file:/backups/db.backup' }`. That serves PVC / RWX users and requires nothing here (see [ADR-015](015-backup-and-restore.md) restore sequencing and [BDR-005](../business/neo4j/005-storage-volume-mode.md) for the RWX `backups` volume). **Cloud identity is only needed when the executor must read/write an object store (`s3://` / `gs://` / `azb://`) directly.**

**Provider matrix in play** ([`dependencies.md`](../../dependencies.md), backlog M-02…M-06): AWS **IRSA** and **EKS Pod Identity**, **GKE Workload Identity** (`iam.gke.io/gcp-service-account`), **Azure Workload Identity** (federated UAMI + pod label + projected token), OpenShift (SCC-bound SA + platform IRSA/WI). Local / kind has no cloud IAM.

**Forces / what breaks if we choose wrong:**

- Pulling a cloud SDK into the operator to sign requests drags provider libraries + their CVE surface into the manager image and RBAC blast radius — [ADR-015](015-backup-and-restore.md) forbids this on purpose. Identity plumbing must stay declarative (SAs, annotations, projected tokens, env), with all object I/O inside `neo4j-admin` (Job) or the Neo4j server (restore).
- Relaxing NEO-002 blindly lets any user bind the workload to an arbitrary cloud IAM role — an escalation surface. The opt-in must be explicit and auditable.
- A WI-only design strands air-gapped / on-prem / local users who have no cloud identity provider; a Secret-only design forces long-lived static keys, which security teams increasingly forbid.

---

## Analysis

The question is narrow: **how does an object-store credential reach the executor** — the Job pod (backup) and the Neo4j server pods (restore)?

### Option A — Static-key Secret projected as env

The user creates a `Secret` with provider keys (`AWS_ACCESS_KEY_ID`, `GOOGLE_APPLICATION_CREDENTIALS`, `AZURE_STORAGE_KEY`, …); the operator projects it as `envFrom` into the Job (already done) and into the workload pods (new).

| Advantages | Disadvantages |
|------------|---------------|
| Portable — works on any cluster, cloud or on-prem | Long-lived static keys; rotation is the user's problem |
| **Locally testable** (MinIO / fake-gcs on kind) | Broad env projection leaks all Secret keys into the pod |
| No per-provider plumbing | Not the production-preferred posture on managed clouds |

### Option B — Kubernetes-native workload identity (keyless)

Bind an annotated ServiceAccount to a cloud IAM principal; the platform (IRSA/Pod Identity/GKE WI/Azure WI webhook) injects short-lived credentials via a projected OIDC token.

| Advantages | Disadvantages |
|------------|---------------|
| Keyless, short-lived, cloud best practice | Per-provider annotations + Azure pod label + projected token |
| No Secret to rotate or leak | **Not locally testable** — needs a real cloud + IAM setup |
| Auditable via cloud IAM | Requires relaxing NEO-002 behind an explicit opt-in |

### Option C — Both, layered (Secret baseline + WI opt-in) — **chosen**

Static-key Secret is the portable, testable default; workload identity is an explicit opt-in for managed clouds. The executor plumbing (which SA, which env, which token) is identical; only the *source* of the credential differs.

| Advantages | Disadvantages |
|------------|---------------|
| Serves on-prem/local **and** managed-cloud users | Two credential paths to document and validate |
| Ship + test the Secret path now; add WI per provider | WI validation can't be gated in-cluster on kind |
| Matches the delegation already written in [BDR-014](../business/backup-restore/014-backup-restore.md) | — |

---

## Comparison

| Criterion | A — Static keys | B — Workload identity | C — Both (chosen) |
|-----------|-----------------|-----------------------|-------------------|
| Portability (on-prem/local) | **yes** | no | **yes** |
| Cloud security posture | weak | **strong** | **strong** (opt-in) |
| Locally testable | **yes** (MinIO) | no | **yes** (Secret path) |
| Operator SDK blast radius | none | none | none |
| Time-to-first-tested-cloud path | **fast** | slow (cloud CI) | **fast** |

---

## Decision

We will support **both** credential models (Option C): a portable **static-key Secret** path and an opt-in **workload-identity** path, plumbed identically to the two executors. The operator carries **no cloud SDK** — it only wires ServiceAccounts, annotations, pod labels, projected tokens, and env.

### Two executors, one identity model

| Executor | Runs | Identity carrier |
|----------|------|------------------|
| Backup **Job** pod | `neo4j-admin database backup` → writes object store | dedicated Job ServiceAccount (annotated for WI) **or** `destination.credentials` Secret as env |
| Neo4j **workload** pods | seed providers read object store during restore | workload ServiceAccount (annotated for WI) **or** a static-key Secret projected as env |

### Backup Job identity

- Give the backup Job pod a **dedicated ServiceAccount** (derived name, e.g. `<neo4j>-backup`), created and owned by the operator (the manager Role already permits `serviceaccounts` create). It **must not** silently use the namespace `default` SA.
- `spec.destination.credentials.secretName` set → project as `envFrom` (unchanged).
- omitted → the Job SA carries the user-supplied WI annotations (see surface below); the platform injects the token. No static keys.

### Workload (restore) identity

- **Relax NEO-002 behind an explicit opt-in.** Cloud-IAM annotations on the workload SA are allowed only when the user has opted in (a dedicated field, not a silent allow), so binding the workload to a cloud role is a deliberate, auditable choice.
- Add the missing surface for WI: a **pod label** channel (Azure requires `azure.workload.identity/use: "true"`) and a **projected service-account token** volume/mount on the workload pods (and Job pods) for providers that consume it directly.
- Add a **static-key surface** for the portable path: allow a referenced `Secret` to be projected as env into the workload container (mirrors the Job's `envFrom`), so cloud restore works without WI (and is MinIO-testable).

### CRD surface (proposed — needs a [BDR-014](../business/backup-restore/014-backup-restore.md) amendment)

The user-facing field shapes are a business concern; this ADR proposes them and defers ratification to a BDR-014 amendment:

- `spec.security.serviceAccount.cloudIdentity` (typed) **or** an explicit `allowCloudIdentityAnnotations: true` gate + the existing `annotations` map — pick one in the amendment; typed is preferred for validation and portability (M-01).
- A backup-family credential/identity block so `Neo4jBackup` / `Neo4jBackupSchedule` can select Secret vs WI for the Job (extends `destination.credentials`).
- Azure pod-label + projected-token knobs (or infer them from the chosen provider).

### Seed-provider / connector configuration

Cloud connector settings the seed providers need (region, endpoint override for S3-compatible stores, etc.) ride on the existing **`spec.config.neo4j`** free-form map ([ADR-013](013-neo4j-conf-directory-fragments.md)); the operator injects no provider config of its own beyond identity. Any required env (`AWS_REGION`, `AWS_ROLE_ARN`, `AWS_WEB_IDENTITY_TOKEN_FILE`, …) is either set by the platform's WI webhook or supplied via the static-key Secret.

### Validation

- Secret vs WI are **mutually exclusive** per executor — CEL `XValidation` on the relevant types (mirror the `RestoreSource` exactly-one-of pattern).
- WI annotations are checked against the existing allowlist (`isCloudWorkloadIdentityAnnotation`); anything off-list stays rejected.
- `Neo4jBackup` / `Neo4jRestore` have **no admission webhook** (CEL + reconciler-time only), while `Neo4j` has the `ValidateNeo4j` chain. Cloud-identity validation therefore lives in CEL on the backup/restore types and in `ValidateSecurity` for the workload; a webhook is **not** added unless a cross-object lookup (e.g. verifying a referenced Secret/SA) proves necessary.

### Provider scope

| Provider | Mechanism | V1 target |
|----------|-----------|-----------|
| Static keys (any, incl. MinIO) | Secret → env | **yes** (testable) |
| AWS | IRSA **and** EKS Pod Identity | yes |
| GCP | GKE Workload Identity | yes |
| Azure | Azure Workload Identity (pod label + projected token) | yes |
| OpenShift / ROSA | SCC-bound SA + platform IRSA/WI | later (M-05) |

### Explicitly out of scope

- The **credential-free `file:`/`server:` restore path** and the **RWX `backups` volume** (they serve PVC users with zero cloud identity — owned by [ADR-015](015-backup-and-restore.md) restore + [BDR-005](../business/neo4j/005-storage-volume-mode.md)).
- Any **object-store SDK in the operator** (forbidden by [ADR-015](015-backup-and-restore.md)).
- Continuous / WAL backup.

---

## Consequences

### Positive

- On-prem/local **and** managed-cloud users are served; nobody is forced into static keys or into a specific cloud.
- The static-key path ships and is **testable now** (MinIO on kind); WI lands per provider on cloud CI without reworking the executor plumbing.
- The operator stays SDK-free; identity is declarative K8s primitives only.
- Reuses seams already in the tree (Job `envFrom`, per-instance workload SA + annotation copy, WI annotation detector), so the diff is plumbing, not new subsystems.

### Negative

- Two credential paths to document, validate, and support.
- Relaxing NEO-002 widens the workload's potential trust surface — mitigated by an explicit opt-in and the annotation allowlist.
- Workload-identity paths are **not** locally testable; correctness depends on cloud CI (the repo already exercises GKE WI federation, Azure SP, and EKS Pod Identity in CI — precedent, but cloud-only).
- Azure WI needs a pod label + projected token, i.e. new pod-level spec surface that other providers don't use.

### Neutral

- The backup Job gaining a dedicated SA is strictly better hygiene than the current `default` SA, independent of cloud identity.
- Static-key restore reuses the same env-projection idiom as backup — no new primitive.
- OpenShift identity is deferred without blocking the three major clouds.

---

## References

- [ADR-015](015-backup-and-restore.md) — backup/restore execution (delegates cloud identity here) · [BDR-014](../business/backup-restore/014-backup-restore.md) — CRD contract
- [ADR-001](001-crd-validation-process.md) — CEL vs webhook · [ADR-013](013-neo4j-conf-directory-fragments.md) — neo4j.conf fragments · [BDR-005](../business/neo4j/005-storage-volume-mode.md) — storage volumes (RWX `backups`)
- [`dependencies.md`](../../dependencies.md) — platform identity matrix (AKS WI · GKE WI · EKS IRSA/Pod Identity · OpenShift SCC)
- Backlog `M-02…M-06` — `.cursor/skills/operator-architecture-orchestrator/architecture-backlog.md`
- [Neo4j — seed from URI (seed providers: file / server / s3 / gs / azb)](https://neo4j.com/docs/operations-manual/current/database-administration/standard-databases/seed-from-uri/)
- [AWS IRSA](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) · [EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html) · [GKE Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) · [Azure Workload Identity](https://azure.github.io/azure-workload-identity/docs/)
