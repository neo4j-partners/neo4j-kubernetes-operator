# Problem statement

Teams running Neo4j on Kubernetes today rely primarily on the **official Helm chart**. That model is proven for installs but creates operational friction as deployments grow beyond a single server.

## Pain points with the Helm model

| Today (Helm) | Impact |
|--------------|--------|
| **One Helm release per cluster member** | N releases to install, upgrade, and observe an N-node cluster |
| **Manual `ENABLE SERVER`** via operations Job | Scale-out requires chart-specific jobs or manual Cypher |
| **~780-line `values.yaml`** with cross-field dependencies | High cognitive load; easy to misconfigure |
| **Scattered concerns** — topology, TLS, ports, storage, plugins in one flat values tree | Hard to validate intent; weak separation of “what I want” vs “how Helm renders it” |
| **`disableLookups` for GitOps** | Template-time cluster lookups break Argo CD / Flux workflows |
| **No unified `.status`** on a single cluster object | Automation and support must aggregate many releases |
| **No continuous reconciliation** | Drift on StatefulSets, Services, or ConfigMaps is not corrected automatically |

GitOps (Argo CD, Flux) can deploy Helm releases reliably, but **does not replace** a domain-specific controller that understands Neo4j cluster formation, member enablement, and pool-aware scaling.

## Quantified pain (reference benchmark)

Industry operator PRDs cite the following gaps when teams hand-roll or Helm-only deploy Neo4j on Kubernetes. This project targets the same outcomes **phased** — V1 addresses provisioning, drift, and scale; backup, upgrade, and security CRDs are post-V1.

| Self-managed pain | Operator direction | V1 |
|-------------------|-------------------|-----|
| Complex bootstrap (STS, Services, discovery, init) | One `Neo4j` CR creates pools, Services, storage | **Yes** |
| Day-2 config drift across environments | Declarative spec + continuous reconcile | **Yes** |
| Patch/minor upgrades with RAFT risk | Orchestrated rolling upgrade | V2+ |
| Backups via cron + static cloud keys | `Neo4jBackup` CRD + pod identity | V2+ |
| Ad-hoc Cypher security scripts | `Neo4jUser` / `Role` / `Grant` CRDs ([BDR-012](../02-technical-design/decision-records/business/012-identity-management.md)) | V2+ |
| Non-Git users blocked by YAML | Optional Web UI wizards | Roadmap |

## What we are solving

A **Kubernetes operator** that:

1. Models **one Neo4j deployment = one CR** (`Standalone` or `Cluster`).
2. **Reconciles** child resources to match declared spec and repairs drift.
3. **Automates cluster day-1/day-2** flows that Helm leaves to operators (member enablement, per-pool scale in V1).
4. Exposes a **first-class status model** for readiness, errors, and topology.

## What we are not solving in V1

See [`04-non-goals.md`](04-non-goals.md). V1 is an **MVP** — not a full replacement for every Helm capability on day one.

## Context

This product definition is authored from **Professional Services (PS)** field experience. Long-term production ownership requires **Product Engineering** partnership — see [`11-risks.md`](11-risks.md) § Organizational constraint.
