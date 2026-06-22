---
name: verify-journey
description: Fresh-eyes pre-release verification — build the operator from current main, install it on a clean Kind cluster, and walk the documented standalone → cluster → sharding phase plan end to end, following docs/developer_guide/release_verification.md.
---

# Verify the user journey (fresh eyes)

Pretend you are a new user who only has the published docs. Build from current
`main`, follow the docs **verbatim**, and report every place reality diverges
from the docs. Automated tests validate the code against itself; this validates
the *published instructions* against a clean machine — the only check that
catches docs that lie. Past passes caught a dead image tag, a `kind: Cluster`
restore gap, a tutorial ordering bug, and a sharded CR-name/logical-name backup
mismatch.

## What to test — follow the canonical matrix

**The phase plan, the capability routing (which scenario runs on standalone vs.
cluster vs. sharding), the sizes, and the per-scenario in-DB checks live in
[`docs/developer_guide/release_verification.md`](../../../docs/developer_guide/release_verification.md).**
Read it and follow it verbatim — it is the single source of truth and is kept
current as the product evolves. This SKILL holds only the *mechanics* below.

Non-negotiables from that doc (repeated here because they bite hardest):

- **KIND only. Enterprise images only** (`neo4j:*-enterprise`).
- **One Enterprise deployment in the cluster at a time** — phases run
  sequentially with **full teardown between them**. Concurrent standalone +
  cluster + sharding JVMs wedge Bolt on a laptop VM (HTTP probe says `Ready`,
  ports 7687/6362 time out).
- **Follow the published docs literally** for Phases 1–2; a wrong/out-of-order
  step *is* a finding — record the exact doc location, don't silently fix it.
- Treat `docs/user_guide/` as read-only here.

## Build & install (once, before Phase 1)

1. **Wipe the slate.** `kind delete clusters --all`

2. **Bring up a Kind cluster + operator built from current `main`.** The
   reliable path is the project's own bootstrap, which also installs
   cert-manager + the `ca-cluster-issuer` the TLS scenarios need:

   ```bash
   git switch main && git pull --ff-only
   make dev-up        # creates the neo4j-operator-dev Kind cluster, installs
                      # cert-manager + CRDs, builds & deploys the main operator
   ```

   We test `main`, so the operator is built from the working tree (the published
   Helm chart is the *previous* release). If you instead follow
   `installation.md` Helm steps verbatim, point them at the locally built image
   — a dead/typo'd tag in those docs is still a real finding.

   !!! note "Kind image-load race (manual `kind load` path)"
       If you load an image manually rather than via `make dev-up`, containerd
       restarts inside the node right after the cluster reports Ready, so the
       first `kind load` often fails (`containerd.sock: connection refused` /
       `content digest not found`). Retry up to 3× with a settle sleep — the
       pattern `scripts/install-confidence.sh` uses.

3. Confirm the operator Deployment is `Ready` and the `ca-cluster-issuer` is
   `True` before starting Phase 1.

## Phase 3 prerequisite (sharding)

Before Phase 3, patch the operator to relax the sharding memory floor (DEV/TEST
only) so it fits a laptop, per the matrix doc:

```bash
kubectl -n <operator-ns> set env deployment/<operator-deploy> NEO4J_SHARDING_RELAX_MEMORY_MIN=true
kubectl -n <operator-ns> rollout status deployment/<operator-deploy>
```

## Report & tear down

- **Report findings as your final message** (not a file): per phase/scenario
  PASS/FAIL, and for every divergence the exact doc location + observed vs.
  documented behavior.
- Add a row to the **Verification log** table in the matrix doc for this run.
- Final teardown: `make dev-down` (or `kind delete cluster --name <name>`).

## Why this exists / provenance

Distilled from real fresh-eyes passes. The standalone → cluster(3) →
sharding(2026.04) phase plan and the one-deployment-at-a-time anti-wedge rule
were settled after a pass where concurrent standalone + cluster JVMs wedged Bolt
on the laptop VM. The full, evolving catalog is the matrix doc linked above —
update *that* (not just this SKILL) when the product grows.
