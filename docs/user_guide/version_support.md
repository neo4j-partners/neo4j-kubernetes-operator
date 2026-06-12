# Supported Neo4j Versions

Which Neo4j versions the operator runs, what's **guaranteed** vs **best-effort**,
how long your version is supported, and what happens when a new release lands.
The design goal: a small, predictable support window that **never lags behind
the database it manages**, so you can plan upgrades on your own schedule.

## Which version should I run?

| Your situation | Run this | Why |
|---|---|---|
| **Mission-critical, strict change control, SLAs** | **5.26 LTS** (`neo4j:5.26-enterprise`) | Feature-stable, fixes-only, longest support lifecycle. Nothing new churns under you. |
| **Want the latest features; agile / non-mission-critical** | the **operator-validated CalVer anchor** (named in each operator release, e.g. `2026.04`) | Newest capabilities, validated by the operator's CI. |
| **Evaluating / dev / test** | either | CalVer for newest features, LTS for stability. |

Enterprise Edition images only — Community is not supported. Versions older than
5.26 (4.x, 5.0–5.25) are not supported.

## Support lifecycle — what you can plan around

- **5.26 LTS** is supported for the duration of Neo4j's own LTS lifecycle
  (~3.5 years from its GA — roughly **mid-2028**; see Neo4j's published support
  schedule for authoritative dates).
- **You will not be force-migrated.** When the next LTS ships, the operator
  *adds* it but keeps supporting 5.26 until 5.26 reaches Neo4j end-of-life — so
  the operator is **never stricter than the database**. You get the full vendor
  overlap (~1+ year) to plan and test your migration.
- **CalVer** is a stay-current track: Neo4j itself supports only the latest
  CalVer, and so does the operator (see below).

## What "supported" means: validated vs. best-effort

The two tracks carry different guarantees — this matters for production sign-off:

- **Validated (tested, guaranteed):** 5.26 LTS, and **one anchor CalVer** per
  operator release (named in its release notes). These run in the operator's
  integration CI and are what we stand behind.
- **Allowed (best-effort):** any CalVer *newer* than a release's anchor. The
  operator doesn't upper-bound CalVer — auto-detection (`major >= 2025`) means
  newer releases run unchanged — but "runs" is not a tested guarantee.

Why the distinction is load-bearing for the operator specifically: it **emits
config and Cypher that Neo4j strictly validates**
(`server.config.strict_validation.enabled=true`, Cypher 25 DDL). A CalVer
released *after* an operator version can rename/remove a config key the operator
writes, or change a Cypher default — and the pod won't start. So a brand-new,
untested CalVer genuinely *can* break the operator; the fix ships in the next
operator release. **For production, run the validated anchor CalVer (or 5.26
LTS); treat newer CalVers as best-effort until a later operator release validates
them.**

## How non-breaking releases let features arrive gradually

Neo4j's release model is what makes new-version support a smooth ramp rather than
a series of cliffs — a real benefit for enterprise planning:

- **Features are additive.** New capabilities land in CalVer incrementally, and
  previously-available functionality keeps working. The operator adopts each
  feature *when it appears*, gated to the release that introduced it, while older
  versions keep the old path — one codebase, e.g. property sharding gated to
  ≥ 2025.12, `ServerSeedProvider` to ≥ 2026.04, remote-address-resolution
  defaulting on at ≥ 2025.09.
- **Deprecations come with a runway.** Neo4j announces a deprecation only once a
  replacement exists, warns across versions, and removes only a major later. The
  operator adopts the replacement early (gated) and retires the old path lazily
  — e.g. the recreate-database procedure switches `dbms.cluster.recreateDatabase`
  (5.24–2025.03) → `dbms.recreateDatabase` (2025.04+), carrying both until the
  floor moves past the old one.
- **The LTS is a snapshot of CalVer you've already been running.** Because a
  feature line *becomes* the next LTS after ~2 years, everything in a new LTS
  shipped in CalVer months earlier. By the time it's blessed as LTS, the operator
  already supports it — the transition is a config/CI change for you, not a
  migration scramble.

**Caveat:** "non-breaking" is a guarantee about *user-facing* functionality
(queries, drivers, app compatibility). The operator also sits on the *config/ops
emission surface*, where keys can be renamed/removed across CalVers — hence the
best-effort caveat above. Feature adoption ramps smoothly; operator-emitted
config still needs per-CalVer validation.

## Upgrades & version transitions

Neo4j supports any-to-any rolling upgrades within a track (e.g. any 5.x to a
later 5.x in one step, zero-downtime in a cluster), and the operator orchestrates
the rollout. For the mechanics (rolling vs. recreate, pre-upgrade health checks,
store-format upgrades), see the [Upgrades guide](guides/upgrades.md). The LTS→LTS
transition is non-disruptive for you: nothing is dropped until your current LTS
reaches Neo4j EOL.

## The policy (how the support set evolves)

> The operator supports **the current Neo4j LTS line plus the current CalVer
> feature line**. A new LTS is **added** at its GA; the previous LTS is **dropped
> only when it reaches Neo4j end-of-life**, not when the new LTS ships.

Today this maps to **5.26 = the active LTS** + **2025.x/2026.x = the feature
track**. The numbers behind it:

| | LTS (feature-stable) | Feature release (CalVer) |
|---|---|---|
| Cadence | every ~2 years | frequent, cumulative |
| Vendor support lifecycle | ~3.5 years | only until the next release |
| Migration overlap | ~1 year between consecutive LTSs | n/a |
| Path to LTS | — | a feature line becomes the next LTS after ~2 years |

Because the LTS lifecycle (~3.5y) outlasts the LTS cadence (~2y), two LTS lines
are supported by Neo4j at once for ~1.5 years. The operator mirrors that:

- **Steady state = two anchors** (current LTS + current CalVer) — the bounded
  matrix this policy optimises for.
- **Transition = briefly three anchors** (old LTS + new LTS + CalVer) through the
  overlap, back to two when the old LTS reaches EOL.

```
5.26 GA (Dec 2024) ──────────────────────────────── 5.26 Neo4j EOL (~mid 2028)
                              new CalVer LTS GA (~2yr later)
                              │
   operator supports:        │
   5.26 + CalVer ────────────┤ + new LTS  (3 anchors, overlap) ──┐
                             add new LTS                          drop 5.26 here
                                                                  (2 anchors again)
```

## Release quality: the gates every release passes

"Validated" isn't a claim, it's a pipeline. A release of this operator cannot
be published unless all of the following pass — they are wired as blocking
gates, not conventions:

| Gate | What it proves | When it runs |
|---|---|---|
| Unit suite + drift gate | Code behavior pinned by ~thousands of unit tests; CRDs, RBAC, Helm chart, and OLM bundle are regenerated and diffed — published manifests always match the code | Every PR, and again on the release tag |
| Core integration suite | Reconcile contracts (cluster formation, standalone lifecycle, databases, backups) against real Neo4j on Kubernetes — on **both** supported lines (5.26 LTS *and* the pinned CalVer) | Every runtime-affecting PR |
| Extended integration suite | The full matrix: scaling with drain, split-brain recovery, the complete backup/restore matrix (PVC, cloud, chains, hooks), sharding | Nightly, and on the exact commit being tagged |
| Install-confidence gate | Five legs on a fresh cluster: Helm install in cluster **and** namespace-scoped RBAC modes (with a live smoke deployment), Helm upgrade **from the previous published release** including the mandatory CRD refresh, documented-order uninstall with live resources, and the kubectl server-side-apply path | Inside the release pipeline — `build-and-push` is blocked until it passes |
| Signed supply chain | Multi-arch (`amd64`/`arm64`) images signed with Sigstore Cosign keyless signing; OLM bundle validated with operator-sdk | Every release |

Verify an image signature yourself:

```bash
cosign verify ghcr.io/neo4j-partners/neo4j-kubernetes-operator:<tag> \
  --certificate-identity-regexp='github.com/neo4j-partners' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

### Platform compatibility

- **Kubernetes**: 1.32+ (CI runs the matrix on upstream Kubernetes via Kind).
- **Managed clouds (EKS, GKE, AKS)**: supported; cloud backups/restores
  authenticate via each cloud's workload identity (IRSA / GKE Workload
  Identity / Azure Workload Identity) or explicit credential Secrets — see
  [Backup & Restore](guides/backup_restore.md) for per-cloud setup.
- **OpenShift**: install via OLM/OperatorHub; the chart's security contexts
  can defer to SCCs (`podSecurityContextEnabled: false`).
- **NetworkPolicy**: enforced policies require a CNI that implements them
  (e.g. Calico, Cilium); Flannel ignores NetworkPolicies silently.

## For maintainers

**Validation gates in code:**

- **Hard-reject** (version validator): anything older than the current LTS
  (pre-5.26 today) or a line past its Neo4j EOL.
- **Allow, don't block** "newer than the validated anchor" within a supported
  track — a brand-new CalVer must not be rejected the day it ships.
- **CI anchors:** integration suites run `5.26-enterprise` + the latest CalVer.
  Invariant: *exactly two* anchors steady-state; a transition window may run three.

**When the next LTS lands**, the change is small and contained — touch:

- the version validator's allowed/minimum set (`internal/validation/`),
- the CI matrix anchors (`.github/workflows/integration.yml`, `integration-tests.yml`),
- the "Supported Neo4j versions" line in `CLAUDE.md`,
- the matrix at the top of this page.

At the *old* LTS's EOL, raising the floor also lets you delete its now-dead
version gates (e.g. the SemVer-only discovery paths once 5.26 is dropped).

## FAQ

**I'm on 5.26 and a new LTS just shipped — will the next operator drop me?**
No. 5.26 stays supported until Neo4j's own EOL for the 5.26 line (~mid 2028),
regardless of how many operator releases ship in between. You migrate on your
schedule, within the overlap window.

**Do you support every CalVer that ships in a given window?**
No. Each operator release *validates* one anchor CalVer; newer CalVers in the
major are *allowed* (forward-compatible) and usually work, but best-effort —
because the operator emits strictly-validated config/Cypher, a future CalVer can
break it until the next operator release catches up. This matches Neo4j, which
supports only the latest CalVer.

**Can I keep running an older CalVer?**
The operator won't block it, but neither Neo4j nor the operator *supports* an
old CalVer — the model is stay-current. For a stable, long-supported floor, use
5.26 LTS instead.

**Why not just support every LTS forever?**
Each supported line multiplies CI cost, branch maintenance, and inter-version
compatibility surface. Bounding the matrix to "current LTS + feature line"
(briefly +1 during overlap) keeps the operator fast to maintain without ever
lagging the database's lifecycle.
