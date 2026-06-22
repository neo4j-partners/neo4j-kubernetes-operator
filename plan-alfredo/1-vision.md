# 1 — Vision

## In one sentence
A Kubernetes operator that lets a platform/DevOps team **run Neo4j as a self-service, managed-like database on their own cluster** — without anyone on the team needing to be a Neo4j expert.

## Who this is for
**Platform engineers, DevOps, and cluster admins** — not Neo4j DBAs. The people who own the Kubernetes stack and are asked to "give us a Neo4j." They want a reliable service, predictable cost, and a quiet pager. They should never have to learn Neo4j internals to operate it safely.

## The core principle: Neo4j is a black box
The user declares **what** they want — "a 3-node Neo4j cluster, this size, with TLS" — in a single Kubernetes manifest. The operator handles **how**: provisioning, clustering, quorum, upgrades, certificates, storage, teardown. The database engine is an implementation detail behind a clean service contract.

```yaml
# This is the entire mental model the admin needs:
apiVersion: neo4j.com/v1beta1
kind: Neo4j
metadata: { name: my-db }
spec:
  version: "5.x"
  servers: 3          # 1 = standalone, 3+ = HA cluster
  storage: { size: 100Gi }
  tls: { secretName: my-cert }
```

No Cypher, no `neo4j-admin`, no manual StatefulSet surgery, no reading Raft docs to scale safely.

## Why it exists — the three value pillars

### 1. Cost control of *their* stack
- Runs on the customer's **own** Kubernetes (AKS first, then EKS) — no separate managed-service bill, no data leaving their cloud account.
- Right-sized by spec: standalone for dev/small, cluster only where HA is needed. Storage and compute are explicit and visible.
- Clean **decommissioning** — tearing a database down fully reclaims its disks/load-balancers, so nothing keeps billing silently.

### 2. Maintainability
- **Declarative** — the cluster state lives in Git/manifests, not in someone's memory of manual steps. Reviewable, repeatable, auditable.
- **Safe day-2 operations** — version upgrades roll out in the correct order and stop if they'd be unsafe; scaling preserves quorum; the operator self-heals after pod/node failures.
- **Portable** — the same API and the same behavior on Azure today and AWS next, so skills and manifests transfer across clouds.

### 3. Monitoring
- **Prometheus metrics** out of the box for both the operator and Neo4j, so it plugs into the team's existing dashboards/alerts.
- **Clear status** on every resource — `Ready`, `Degraded`, and human-readable conditions/events — so an admin can tell health at a glance and know *why* something is wrong (cert problem vs quorum problem) without opening the database.

## What success looks like
An admin who has never used Neo4j can stand up a production-grade HA cluster from one manifest, see it healthy in their existing monitoring, upgrade it safely, and tear it down cleanly — all without filing a ticket to a Neo4j specialist.

## Non-goals (deliberately out of scope)
- Not a Neo4j tuning/DBA console. No query optimization, no graph-data tooling, no UI in V1.
- Not a multi-tenant fleet platform (no large-scale namespace auto-onboarding) in V1.
- Not a backup product in V1 — backup/restore is V2.
- Not cloud-specific lock-in — cloud differences are isolated to a thin adapter layer.

## Scope at a glance
- **V1 (this 12-week project, Azure):** standalone + HA cluster, ingress, basic platform & Neo4j config, storage, decommissioning, Prometheus metrics, TLS with user-provided certs.
- **V2:** backup & restore, PVC lifecycle, reverse proxy, cert-manager issuance.
- **Later:** AWS adapter (follow-on), then GCP / OpenShift.

→ The detailed scope is in [3-backlog.md](3-backlog.md); the full customer requirement set is [2-product-requirements.csv](2-product-requirements.csv).
