# Executive summary

The Neo4j Kubernetes Operator delivers a **declarative, reconciled lifecycle** for Neo4j on Kubernetes through a single `Neo4j` custom resource — replacing the multi-release Helm model for cluster deployments.

**V1 (MVP)** focuses on the **`Neo4j` CRD only**: deploy Standalone or Cluster, persist data on dynamic PVCs, expose Bolt/HTTP via ClusterIP, scale cluster members per pool, and surface readiness through Kubernetes status. Backup, restore, monitoring, and day-2 CRDs are explicitly deferred.

**Why now**: the existing Helm chart works but does not model a cluster as one object, does not reconcile drift, and scatters operational knowledge across `values.yaml`, per-member releases, and manual `ENABLE SERVER` jobs.

**Strategic value** (adapted from enterprise operator reference material): self-managed customers get **cloud-like ergonomics** — faster provisioning, declarative desired state, portable manifests across AKS/EKS/GKE/OpenShift — while keeping **infrastructure sovereignty**. V1 delivers the foundation (one CR, reconcile, scale); compliance-heavy features (backup automation, declarative DB security, Web UI) follow in phased releases.

**Success depends on** more than code — adoption stoppers (support model, Product Engineering ownership, Helm migration, differentiation) are captured in [`11-risks.md`](11-risks.md) and [`14-open-questions.md`](14-open-questions.md).

| Document | Content |
|----------|---------|
| [`02-problem-statement.md`](02-problem-statement.md) | Why an operator beyond Helm |
| [`03-goals.md`](03-goals.md) | V1 product goals |
| [`04-non-goals.md`](04-non-goals.md) | Out of scope for V1 |
| [`05-personas.md`](05-personas.md) | Target users |
| [`06-user-stories.md`](06-user-stories.md) | Journeys and core workflows |
| [`09-api.md`](09-api.md) | CRD inventory and phasing |
| [`13-roadmap.md`](13-roadmap.md) | V1 / V2 phasing |
| [`07-functional-requirements.csv`](07-functional-requirements.csv) | Traceable requirements (`V1` column) |

Technical specification → [`../02-technical-design/`](../02-technical-design/). Reference PRD source → [`../00-discovery/export.md`](../00-discovery/export.md).
