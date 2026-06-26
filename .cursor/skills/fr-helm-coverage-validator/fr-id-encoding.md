# FR ID encoding reference

Source: `design/readme.md` — FR drill-down section.

## Levels

| Level | Pattern | Example |
|-------|---------|---------|
| 1 | `{PREFIX}-1-{nnn}` | `NEO-1-001` |
| 2 | `{PREFIX}-2-{nnn}` or `{PREFIX}-2-{parent}-{Group}-{nn}` | `NEO-2-006`, `NEO-2-007-PRT-01` |
| 3 | `{PREFIX}-3-{parent}-{Group}-{nn}` | `NEO-3-006-PVC-01` |

Prefixes: `NEO` (workload), `OP` (operator).

## Parent / Requires IDs

- Level-3 rows: `Parent ID` = level-2 capability (e.g. `NEO-2-006`)
- `NEO-1-001` / `NEO-1-002`: `Requires IDs` lists composed level-2 capabilities
- Adding a level-3 FR may require updating parent `Requires IDs` on level-1 roots if new level-2 sibling

## Group codes (from `03-variant_matrix.csv`)

| Code | Domain |
|------|--------|
| EDT | Edition |
| LIC | License |
| MODE | Deployment mode |
| CSZ | Cluster size |
| CFG | Neo4j config |
| JVM | JVM |
| IMG | Image |
| CRED | Credentials |
| SEC | Security |
| TLS | TLS |
| PVC | Persistence / PVC |
| VOL | Volumes |
| CLD | Cloud storage |
| SVC | Services |
| PRT | Ports |
| PCMB | Protocols / combo |
| MULTI | Multi-cluster |
| SCH | Scheduling |
| PROBE | Probes |
| RSTR | Restart strategy |
| SRV | Server enablement |
| UPG | Upgrade |
| BKMD | Backup mode |
| BKST | Backup storage |
| MON | Monitoring |
| LOG | Logging |
| JOB | Jobs |
| MNT | Maintenance |
| APOC | APOC |

## Suggested new ID workflow

1. Find parent level-2 in `01-functional_requirements.csv`
2. Scan existing level-3 under that parent for next `{nn}`
3. Propose row with: `ID`, `Parent ID`, `Requires IDs` (empty for L3), `Domain`, `Config Group`, `Requirement`, `Description`, `Primary AC Groups`, `V1`, `V1 Justification`
4. If no level-2 exists, propose level-2 first, then level-3 children

## Out of scope for FR (document as `out_of_scope`, not missing)

| Helm signal | Rationale |
|-------------|-----------|
| `disableLookups` | Helm/ArgoCD template workaround — not operator feature |
| `nameOverride` / `fullnameOverride` | Kubernetes resource naming — `metadata.name` |
| Chart-internal ops image pins | Operator implementation detail unless user-facing |
