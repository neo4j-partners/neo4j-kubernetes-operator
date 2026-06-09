# Self-Hosted CI Runners (ARC on EKS) — Plan & Recommendations

**Date:** 2026-06-09
**Status:** Plan / decision record (no infra provisioned yet)
**Related:** PR #147 (tiered integration lanes + CI caching), `docs/developer_guide/ci_and_workflows.md`

## Executive Summary

The operator's CI is constrained by GitHub-hosted standard runners (2 vCPU /
7 GB) and the org does **not** have access to GitHub's larger hosted runners.
That ceiling (a) blocks the resource-heavy property-sharding suites from running
in CI at all (they need 4 GB/server and self-skip), (b) forces Neo4j servers to a
500m CPU throttle that slows every spec's formation, and (c) caps concurrency to
a shared org-wide pool as the contributor base grows.

This report recommends moving the **trusted, heavy** CI lanes to **self-hosted,
ephemeral runners via Actions Runner Controller (ARC) on EKS**, in
`containerMode: dind`, while keeping untrusted fork-PR runs on hosted runners.
It covers EKS sizing, ARC scaling, the setup path, the security model, how this
composes with the caching already landed in PR #147, and the expected gains. A
complementary code-level optimization (the unit suite's `TestClient` bottleneck)
is included because it is the cheapest near-term CI win and needs no infra.

## Context / Why

- **KIND-only** test model: every integration spec runs a real Kind cluster with
  the operator deployed and Neo4j Enterprise pods inside it. So "CI infra" means
  *runners that host Kind*, not managed EKS clusters for the tests themselves.
- **RAM is the binding constraint.** Neo4j Enterprise needs ≥ 1.5 GB/pod; the CI
  optimizer shrinks clusters to 2×1.5 GB. Property sharding needs **4 GB/server**,
  which a 7 GB runner cannot provide — so `property_sharding{,_backup,_minio_restore,_pvc_seed}`
  self-skip via `isRunningInCI()` and are **local-only**. No CI lane covers F3/F4/F5
  or sharded backup/restore today.
- **CPU is self-throttled.** `getCIAppropriateResourceRequirements()` /
  `applyCIOptimizations()` cap Neo4j at **100m request / 500m limit** *because* the
  2-vCPU runner is shared with Kind + dind + the operator. Neo4j startup/formation
  is partly CPU-bound (JVM warmup, store/index init, RAFT apply), so this throttle
  slows nearly every spec.
- **Measured baselines (2026-06):**
  - Extended (full) suite on CalVer 2026.04: **48 of 60 specs in ~81 min**, serial
    (12 sharding specs skipped).
  - Unit suite via gotestsum: **1449 tests in ~169 s**, wall-clock gated by two
    packages (see *Complementary optimization* below).
- **Team growth.** More contributors → more concurrent PRs → queueing against the
  shared hosted pool.

The tiered lanes (PR #147) already bound *per-PR* cost (most PRs run only the fast
`core` subset; the full suite is nightly + on coordination-critical PRs). What
remains unaddressed is the **RAM ceiling** (sharding coverage), the **CPU
throttle**, and **dedicated concurrency** — all of which need bigger runners.

## Decision

**Adopt ARC on EKS (`containerMode: dind`) for the trusted heavy lanes**, keeping
fork-PR/untrusted runs on hosted runners.

ARC was initially dismissed in favor of ephemeral EC2 on the assumption that
Kind-in-dind was fiddly. Re-checking the current ARC docs corrected that: modern
`gha-runner-scale-set` has a **first-class `containerMode: dind`** (auto-injected
privileged `dockerd` sidecar, shared socket, MTU shim; native K8s sidecar on
≥ 1.29), and Kind specifically requires dind mode. The remaining trade-offs
(privileged sidecar, Kind-in-dind-in-pod nesting, a standing EKS cluster) are
acceptable **if the org already operates Kubernetes/EKS**. If it does not,
ephemeral EC2 (e.g. `philips-labs/terraform-aws-github-runner`) remains the
lower-ops alternative — same security model, "instance type = RAM," no nesting.
This plan assumes the EKS/ARC path; the sizing and security sections transfer to
the EC2 path with minor changes.

## EKS Sizing

**Control plane / version:** EKS ~$73/mo flat floor. Run **K8s ≥ 1.29** for native
sidecar dind.

**Two node groups — separate always-on from elastic:**

| Node group | Capacity | Purpose | Sizing |
|---|---|---|---|
| **system** (managed, on-demand) | 2 nodes, stable | ARC controller, listener pods, Karpenter, CoreDNS, in-cluster registry mirror | `m6i.large` ×2 |
| **runner** (Karpenter, spot, scale-to-zero) | 0 → N just-in-time | the ephemeral dind runner pods | provisioned per pending pod |

**Why Karpenter** (vs Cluster Autoscaler): the runner workload is heterogeneous
(core ~8 GB vs xl ~24 GB), bursty, and spot/scale-to-zero. Karpenter provisions a
right-sized node per pending pod from a family list with no pre-defined node
groups, scales to zero, and handles spot diversification/interruption — a better
fit than maintaining one ASG per pod shape. *If the org already runs Cluster
Autoscaler and does not want a second autoscaler, two `min=0` managed node groups
(core-sized, xl-sized) are an acceptable, simpler substitute.*

**The pod → node memory math (the load-bearing part).** In dind mode a runner
**pod** holds: the dind sidecar → Kind node container(s) → the Neo4j pods inside
Kind. The pod's memory limit must envelope the entire Kind workload:

| Lane | Neo4j workload | Pod request=limit (Guaranteed QoS) | Lands on |
|---|---|---|---|
| **core** (2×1.5 GB) | ~3 GB | **~8 GB / 4 vCPU**, +20–40 GB ephemeral-storage | `m6i.xlarge` |
| **extended / sharding** (2×4 GB + graph/property DBs) | ~8–12 GB | **~20–24 GB / 6–8 vCPU**, +40 GB ephemeral-storage | `r6i.2xlarge` (8/64) |

Non-negotiables:
- **memory request == limit** (Guaranteed QoS) — Burstable + Neo4j formation = OOMKill.
- **ephemeral-storage + node disk**: Kind layers + Neo4j image + DB data live on
  the node; set the pod's `ephemeral-storage` request and a generous Karpenter
  EC2NodeClass root volume (≥ **100 GB gp3**) or `kind create` fails on disk pressure.
- dind sidecar gets its own modest request (~1–2 GB / 1 vCPU); the heavy memory is
  the workload running through it.

## ARC Scaling

**Two `gha-runner-scale-set` installs, mapped to the lanes:**

| Scale set (`runs-on`) | `minRunners` | `maxRunners` | Pod size | For |
|---|---|---|---|---|
| `arc-core` | **1–2** (warm → fast PR feedback) | 6–10 | ~8 GB | core lane, trusted PRs |
| `arc-xl` | **0** (scale-to-zero) | 2–4 | ~20–24 GB | Extended + sharding, nightly/trusted |

- **Ephemeral runners** (default) — one job per runner, destroyed after. Strong
  isolation; don't disable.
- `containerMode.type: dind` for both (Kind needs it).
- Autoscaling is **job-queue-driven** (the listener scales runners to assigned
  jobs); `maxRunners` is the concurrency cap. No HPA tuning.
- `minRunners: 1–2` on `arc-core` avoids Karpenter cold-start on every PR;
  `minRunners: 0` on `arc-xl` (rare jobs, cold-start tolerable).
- **PodSecurity:** dind requires `privileged: true`, so the runner namespace must
  be labeled `pod-security.kubernetes.io/enforce: privileged`. Isolate it.

## Setup Path (ordered)

1. **Auth:** create a **GitHub App** (not a PAT) scoped to the repo/org with
   Actions read/write; store creds in a `githubConfigSecret`.
2. **Install the ARC controller** (Helm, OCI):
   `oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller`
   into `arc-systems`. Pin a chart version.
3. **Install two scale sets** from `…/gha-runner-scale-set` (`arc-core`, `arc-xl`),
   each with `githubConfigUrl`, `githubConfigSecret`, `containerMode.type=dind`,
   `minRunners`/`maxRunners`, and a runner pod `template` carrying the resource
   requests/limits + `ephemeral-storage` + nodeSelector/tolerations for the
   Karpenter spot nodes. The install name is the `runs-on` label.
4. **Karpenter:** install; `NodePool` (spot, family list, consolidation, CPU/mem
   ceiling) + `EC2NodeClass` (AL2023, **private subnets**, locked SG, 100 GB gp3).
5. **IAM — keep it empty.** Tests are Kind-local and use **MinIO, not real S3**, so
   the runner ServiceAccount needs **no AWS data-plane IAM**. Karpenter/node roles
   get their own, separate from the runner SA. Empty runner IAM = near-zero blast
   radius.
6. **Security gating** (see below).
7. **Wire `runs-on`:** core lane → `arc-core` on trusted triggers (hosted on fork
   PRs); Extended/nightly → `arc-xl`.
8. **Unlock sharding:** have `arc-xl` runners export `SHARDING_CAPABLE=true` and
   change the sharding `Skip` from "skip if `isRunningInCI()`" to "skip unless
   `SHARDING_CAPABLE`" — so the heavy suites run on `arc-xl` (4 GB/server headroom)
   but still skip on hosted/standard CI.

## Security Model (the dominant constraint)

The repo is public and takes fork PRs, and the contributor base is growing — which
makes self-hosted runners *more* dangerous, not less. A fork PR on a privileged
dind runner = arbitrary code with the runner's IAM, network, and caches.

- **Ephemeral only** (fresh pod per job).
- **Do not run fork PRs on self-hosted.** Route untrusted runs to hosted standard
  runners; reserve `arc-*` for **trusted triggers**: `push`/nightly on `main`, and
  maintainer-approved PRs.
- Route on **trust**, not naive fork detection, so a trusted teammate working from
  a fork is not blocked:
  ```yaml
  runs-on: >-
    ${{ (github.event.pull_request.head.repo.fork == false ||
         contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'),
                  github.event.pull_request.author_association))
        && 'arc-xl' || 'ubuntu-latest' }}
  ```
- **The internal team does not hit this block.** Members with push access open
  *branch* PRs (same-repo), which run with full access and need no approval. Only
  *fork* PRs (external contributors) are gated. Set repo "Require approval for fork
  PRs" to *first-time contributors*.
- **Never use `pull_request_target`** to grant fork PRs self-hosted access — that
  runs fork code in the trusted, secret-bearing context.
- Minimal/empty runner IAM, private subnet, locked SG, short-lived repo-scoped
  registration tokens.

## How This Composes With the Caching Already Landed (PR #147)

PR #147 added: `./bin` tool + envtest caching (via the `setup-go` composite),
Go build cache (setup-go `cache:true`), operator-image gha layer cache (buildx),
and a Neo4j image **tarball** cache (`actions/cache`). On self-hosted these still
work, and two get materially better:

- **In-cluster registry pull-through mirror** (run `registry:2` or Harbor on the
  system node; point dind at it via `--registry-mirror`). All ephemeral runners
  then pull Neo4j/Kind images same-AZ, cached, un-throttled — superior to the
  per-job `actions/cache` tarball, and survives scale-to-zero. This is the
  standout self-hosted caching win; fold the existing tarball logic into it.
- **Prebaked runner image / warm node** can carry the Go toolchain + pinned tools,
  subsuming the `./bin` cache.

Net: the caching work was the right "today, hosted" move (speed + kills the Docker
Hub / `sum.golang.org` flake classes) and remains valuable on self-hosted, with
the registry mirror as the upgrade path.

## Expected Gains

Honest framing: the integration tests are **latency-bound**, not purely CPU-bound,
so raw speedups are bounded; the bigger wins are coverage, concurrency, and the
parallelism the extra RAM unlocks.

- **Speed (per run):**
  - *Vertical* (more vCPU/RAM, still serial): ~1.3–1.6× on the CPU/mem-bound slice
    (compile, JVM warmup, store init); image pulls and RAFT timers don't move.
  - *Un-throttle Neo4j CPU* (raise the 500m cap to 1–2 cores on big runners): real
    gains on formation/startup, **quantized by the 5 s `Eventually` poll** — lower
    the interval on fast runners to capture it.
  - *Ginkgo parallelism* (`-p`): the big lever, **gated by RAM** (N concurrent Neo4j
    clusters) and by parallelizable specs. **Core lane** parallelizes well after
    refactors; **Extended** is gated by 8 `Serial` files (sharding/backup) and
    benefits more from the CPU bump than from `-p`.
  - Realistic full-suite wall-clock: ~81 min → ~35–55 min, mostly from parallelism
    + the registry mirror.
  - *Prerequisite for `-p`:* convert `BeforeSuite` → `SynchronizedBeforeSuite`
    (today N processes would each install CRDs / wait for the operator) and make
    `createTestNamespace` include `GinkgoParallelProcess()`.
- **Concurrency:** from a shared, capped GitHub pool (queues under load) to
  dedicated `maxRunners` bounded by AWS budget — queueing effectively disappears
  (cold-start ~30–60 s when scaling from zero).
- **Coverage (the real prize):** the property-sharding suites go **0 → covered** in
  CI via `arc-xl` + `SHARDING_CAPABLE`.

## Cost

EKS control plane ~$73/mo + spot node-hours (a nightly Extended run on a spot
`r6i.2xlarge` is cents) + a couple of small warm `arc-core` pods. The real cost is
**setup + maintenance** (Karpenter, the registry mirror, runner-image upkeep), not
compute.

## Rollout Phases

1. **Stand up EKS + ARC** with the two scale sets, Karpenter spot, privileged
   namespace, **empty runner IAM**, and the **trusted-trigger firewall**. Validate
   the core lane on `arc-core` first.
2. **Add the in-cluster registry mirror**; point dind at it.
3. **Flip `arc-xl` + the `SHARDING_CAPABLE` gate** — close the sharding coverage gap.
4. **(Optional) Enable Ginkgo `-p`** on the core lane after the
   `SynchronizedBeforeSuite` + parallel-namespace refactors.

## Complementary Optimization (no infra needed): the unit-suite bottleneck

Per-package timing (now emitted by the gotestsum summary in CI) shows the unit
suite's ~169 s wall-clock is gated by two packages, because `go test`
parallelizes *across* packages so wall-clock ≈ the slowest package:

- **`internal/neo4j` `TestClient` — ~160 s.** Root cause: `client_test.go` specs
  call `VerifyConnectivity()` (and a circuit-breaker loop) against a cluster with
  **no real Neo4j**, so each call waits out the Bolt driver's real
  `SocketConnectTimeout`/`ConnectionAcquisitionTimeout` (5–10 s) for a TCP timeout.
  ~160 s of pure network waiting — wall-clock, not CPU.
- **`internal/controller` `TestControllers` — ~37 s** (envtest control-plane
  startup + specs). The next floor once `TestClient` is fixed.

**What to do about `TestClient`** (highest-leverage, cheapest CI win):
1. **Make the failure instant.** Point the test client at a guaranteed-**refused**
   address (e.g. `localhost:<closed port>` → immediate `ECONNREFUSED`) instead of
   one that black-holes, *or* inject sub-second driver timeouts for tests. Expected:
   ~160 s → a few seconds.
2. **Or mock connectivity** behind an interface seam so unit specs do no real
   network (best for true unit isolation).
3. **Or Ginkgo-parallelize** the suite (`--procs`) so the waits overlap →
   wall-clock ≈ the slowest single spec. Cheap, but treats the symptom.

Impact: fixing `TestClient` drops the unit suite from ~169 s toward ~37 s (then
`TestControllers` becomes the floor). This is a small, self-contained follow-up PR,
independent of the runner infra.

## Open Decisions / Risks

- **EKS or ephemeral EC2?** ARC if the org already operates Kubernetes; ephemeral
  EC2 if not (lower ops for a Kind-only workload).
- **Privileged dind** must be allowed by cluster PodSecurity policy.
- **Fork-PR coverage gap:** external contributors touching backup/restore/sharding
  won't get the heavy lanes until a maintainer runs them — acceptable, by design.
- **CalVer pin upkeep:** the pinned tags live in two places (the `integration.yml`
  matrix list and the `integration-tests.yml` input default) and must move together.
