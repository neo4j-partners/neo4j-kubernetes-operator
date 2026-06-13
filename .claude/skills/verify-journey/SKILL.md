---
name: verify-journey
description: Fresh-eyes verification — build the operator from current main, install it per the published docs on a clean Kind cluster, and walk the real user scenarios end to end.
---

# Verify the user journey (fresh eyes)

Pretend you are a new user who only has the published docs. Build from the
current tree, follow the docs **verbatim**, and report every place reality
diverges from the docs. This pass has historically caught a dead image tag, a
`kind: Cluster` restore gap, and a tutorial ordering bug — bugs that unit/
integration tests did not surface because they don't read the docs.

## Ground rules

- **KIND ONLY.** Never minikube/k3s. **Enterprise images only**
  (`neo4j:*-enterprise`), never community.
- **Follow the docs literally.** If a step is wrong or out of order, that IS the
  finding — do not silently "fix" it in your head. Note the exact doc location
  and what actually had to happen.
- Treat `docs/user_guide/` as read-only reference here (you verify against it;
  you do not edit it).

## Procedure

1. **Wipe the slate.**
   ```bash
   kind delete clusters --all
   ```

2. **Build the operator image from the current `main`.**
   ```bash
   git switch main && git pull --ff-only
   docker build -t neo4j-operator:journey .
   ```

3. **Create a Kind cluster and load the image — RETRY the load.** Kind/
   containerd restarts inside the node right after the cluster reports Ready,
   so the first `kind load` often fails with
   `containerd.sock: connection refused` (or `content digest not found`). Retry
   up to 3x with a settle sleep — the same pattern `scripts/install-confidence.sh`
   uses:
   ```bash
   kind create cluster --name neo4j-journey --wait 120s
   for attempt in 1 2 3; do
     kind load docker-image neo4j-operator:journey --name neo4j-journey && break
     [ "$attempt" = 3 ] && { echo "kind load failed after 3 attempts"; exit 1; }
     echo "load attempt $attempt failed (containerd settling); retrying in 10s"; sleep 10
   done
   ```

4. **Install the operator following `docs/user_guide/installation.md` verbatim.**
   Use the Helm path the docs lead with. Wait for the operator Deployment to be
   Ready before proceeding. If the docs reference an image tag, use exactly
   what they say (a dead/typo'd tag here is a real finding).

5. **Walk the common scenarios, each to `Ready`, following the published docs
   in order.** Deploy and confirm each reconciles to Ready, then verify
   *inside the database* (not just the CR status):
   - **Standalone** (`Neo4jEnterpriseStandalone`) → Ready.
   - **Cluster** (`Neo4jEnterpriseCluster`, ≥2 servers) → Ready; pods are
     `{cluster}-server-0..N-1`.
   - **Database** (`Neo4jDatabase`) → Ready; confirm via
     `cypher-shell ... 'SHOW DATABASES'`.
   - **Users / roles** (`Neo4jUser` / `Neo4jRole` / `Neo4jRoleBinding`) →
     confirm via `SHOW USERS` / `SHOW ROLES`.
   - **Plugin** (`Neo4jPlugin`, e.g. APOC) → confirm via
     `RETURN apoc.version()`.
   - **Backup → restore** following the published backup/restore tutorial
     steps exactly. This is where ordering/gap bugs hide — note the exact step
     where anything had to deviate.

   Useful in-DB check pattern (per CLAUDE.md):
   ```bash
   kubectl exec <pod> -c neo4j -- cypher-shell -u neo4j -p <password> "SHOW DATABASES"
   ```

6. **Report findings as your final message** (not a file): for each scenario,
   PASS/FAIL, and for every divergence the exact doc location + observed vs.
   documented behavior. Flag dead image tags, missing/incorrect steps, wrong
   ordering, and any scenario the docs imply works but doesn't (e.g. a
   `kind: Cluster` restore path the docs don't actually cover).

7. **Tear down.**
   ```bash
   kind delete cluster --name neo4j-journey
   ```

## Why this exists / provenance

Distilled from a real fresh-eyes verification pass that caught a dead image
tag, a `kind: Cluster` restore gap, and a tutorial ordering bug. Automated
tests validate the code against itself; this skill validates the *published
instructions* against a clean machine — the only check that catches docs that
lie. The 3x kind-load retry is not optional: it is a deterministic
containerd-restart race reproduced on recent Docker + kind.
