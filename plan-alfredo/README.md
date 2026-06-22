# Neo4j Kubernetes Operator — Azure-first (V1)

Greenfield operator that runs Neo4j as a self-service database for platform/DevOps teams. **Azure first, AWS second**, maximum shared code. V1 = 12 weeks, ~1.2 FTE.

| # | File | What it is |
|---|---|---|
| 1 | [1-vision.md](1-vision.md) | Why this exists — for admins/DevOps; Neo4j as a black-box service; cost, maintainability, monitoring |
| 2 | [2-product-requirements.csv](2-product-requirements.csv) | The customer's requirements table (278 rows) — source of truth |
| 3 | [3-backlog.md](3-backlog.md) | Final plan (non-technical): the 57-item V1 backlog + 12-week timeline |
| 4 | [4-technical-shared.md](4-technical-shared.md) | Technical: the cloud-agnostic shared core + the `Platform` seam |
| 5 | [5-technical-azure.md](5-technical-azure.md) | Technical: the Azure (AKS) adapter — V1 target |
| 6 | [6-technical-aws.md](6-technical-aws.md) | Technical: the AWS (EKS) adapter — follow-on |

Traceability: backlog (#3) and technical items reference the customer table (#2) via `Source ID` (`T####` / `GAP-###` / `NEW-###`). The customer table (#2) also carries a **`V1/V2 Scope`** column (V1 / V1-Hardening / V2 / Out / AWS-followon / GCP-deferred / OpenShift-deferred) + the matching **`V1 ID`** — so every requirement's disposition is on its own row.
